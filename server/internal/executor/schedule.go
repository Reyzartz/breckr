package executor

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"breckr-server/internal/types"
	"breckr-server/internal/utils"

	"github.com/robfig/cron/v3"
)

/*
The two directions between a form's schedule and a cron expression.

Cron is the storage format and this is the only place that reads or writes one
on a task's behalf -- the dashboard exchanges Schedule objects and never builds
an expression itself, so there is a single implementation of the mapping rather
than one per side that can drift.

FromCron is total on purpose. Every stored row has to open in the form,
including expressions this file cannot express structurally, so anything
unrecognized comes back as `custom` carrying the original text. That is what
keeps editing a task's name from quietly rewriting its schedule.

Pure: no database, no config, no clock.
*/

const scheduleField = "schedule"

// weekdayMax is cron's own day numbering, Sunday first.
const weekdayMax = 6

// Parser accepts the same expressions node-cron did: five fields, or six with a
// leading seconds field, plus the @descriptors. The `custom` escape hatch means
// arbitrary user expressions reach it, so the acceptance set has to match or
// FromCron's totality assumption breaks.
var Parser = cron.NewParser(
	cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
)

// ValidateCronExpr reports whether the parser will accept an expression.
func ValidateCronExpr(expr string) bool {
	_, err := Parser.Parse(expr)
	return err == nil
}

func requireInt(value *int, label string, min, max int) (int, error) {
	if value == nil {
		return 0, utils.Fail(scheduleField,
			"%s must be a whole number between %d and %d, got null.", label, min, max)
	}
	if *value < min || *value > max {
		return 0, utils.Fail(scheduleField,
			"%s must be a whole number between %d and %d, got %d.", label, min, max, *value)
	}
	return *value, nil
}

func requireWeekdays(value []int) ([]int, error) {
	if len(value) == 0 {
		return nil, utils.Fail(scheduleField, "`weekdays` must list at least one day.")
	}

	seen := map[int]bool{}
	days := []int{}
	for _, day := range value {
		d := day
		checked, err := requireInt(&d, "`weekdays`", 0, weekdayMax)
		if err != nil {
			return nil, err
		}
		if !seen[checked] {
			seen[checked] = true
			days = append(days, checked)
		}
	}

	// Sorted and deduped so two spellings of the same week produce one
	// expression, and so FromCron's output compares equal to what went in.
	sort.Ints(days)
	return days, nil
}

// ToCron converts a schedule to cron, validating it on the way.
//
// This is also the validator for untrusted input: every field is range-checked
// here, so a body that decoded into a Schedule still cannot produce an
// expression the cron parser would reject at schedule time.
func ToCron(schedule *types.Schedule) (string, error) {
	if schedule == nil {
		return "", utils.Fail(scheduleField, "`schedule` must be an object.")
	}

	switch schedule.Every {
	case types.FreqMinutes:
		interval, err := requireInt(schedule.Interval, "`interval`", 1, 59)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("*/%d * * * *", interval), nil

	case types.FreqHours:
		interval, err := requireInt(schedule.Interval, "`interval`", 1, 23)
		if err != nil {
			return "", err
		}
		minute, err := requireInt(schedule.Minute, "`minute`", 0, 59)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%d */%d * * *", minute, interval), nil

	case types.FreqDay:
		hour, err := requireInt(schedule.Hour, "`hour`", 0, 23)
		if err != nil {
			return "", err
		}
		minute, err := requireInt(schedule.Minute, "`minute`", 0, 59)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%d %d * * *", minute, hour), nil

	case types.FreqWeek:
		weekdays, err := requireWeekdays(schedule.Weekdays)
		if err != nil {
			return "", err
		}
		hour, err := requireInt(schedule.Hour, "`hour`", 0, 23)
		if err != nil {
			return "", err
		}
		minute, err := requireInt(schedule.Minute, "`minute`", 0, 59)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%d %d * * %s", minute, hour, joinInts(weekdays)), nil

	case types.FreqMonth:
		day, err := requireInt(schedule.Day, "`day`", 1, 31)
		if err != nil {
			return "", err
		}
		hour, err := requireInt(schedule.Hour, "`hour`", 0, 23)
		if err != nil {
			return "", err
		}
		minute, err := requireInt(schedule.Minute, "`minute`", 0, 59)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%d %d %d * *", minute, hour, day), nil

	case types.FreqCustom:
		if schedule.Cron == nil || strings.TrimSpace(*schedule.Cron) == "" {
			return "", utils.Fail(scheduleField, "`cron` must be a non-empty string.")
		}
		expr := strings.TrimSpace(*schedule.Cron)
		if !ValidateCronExpr(expr) {
			return "", utils.Fail(scheduleField, "%q is not a valid cron expression.", expr)
		}
		return expr, nil

	default:
		return "", utils.Fail(scheduleField,
			"`every` must be one of %s, got %q.",
			strings.Join(types.ScheduleFrequencies, ", "), schedule.Every)
	}
}

func joinInts(values []int) string {
	parts := make([]string, len(values))
	for i, value := range values {
		parts[i] = strconv.Itoa(value)
	}
	return strings.Join(parts, ",")
}

var (
	plainIntPattern = regexp.MustCompile(`^\d+$`)
	stepPattern     = regexp.MustCompile(`^\*/(\d+)$`)
	rangePattern    = regexp.MustCompile(`^(\d)-(\d)$`)
)

// plainInt matches a bare non-negative integer. Ranges, lists and steps
// deliberately fail.
func plainInt(field string) (int, bool) {
	if !plainIntPattern.MatchString(field) {
		return 0, false
	}
	value, err := strconv.Atoi(field)
	return value, err == nil
}

// stepOf matches the `*/N` step form.
func stepOf(field string) (int, bool) {
	match := stepPattern.FindStringSubmatch(field)
	if match == nil {
		return 0, false
	}
	value, err := strconv.Atoi(match[1])
	return value, err == nil
}

// parseWeekdays expands a day-of-week field's lists and ranges, reporting false
// if it uses anything else.
func parseWeekdays(field string) ([]int, bool) {
	seen := map[int]bool{}

	for _, part := range strings.Split(field, ",") {
		if match := rangePattern.FindStringSubmatch(part); match != nil {
			from, _ := strconv.Atoi(match[1])
			to, _ := strconv.Atoi(match[2])
			// Cron allows a wrapping range like 5-1; the builder has no way to
			// show one, so it stays custom rather than being silently reordered.
			if from > to || to > weekdayMax {
				return nil, false
			}
			for day := from; day <= to; day++ {
				seen[day] = true
			}
			continue
		}

		single, ok := plainInt(part)
		// Names (MON) and 7-for-Sunday are valid cron but have no control here.
		if !ok || single > weekdayMax {
			return nil, false
		}
		seen[single] = true
	}

	if len(seen) == 0 {
		return nil, false
	}

	days := make([]int, 0, len(seen))
	for day := range seen {
		days = append(days, day)
	}
	sort.Ints(days)
	return days, true
}

// FromCron derives the schedule a cron expression came from.
//
// Total: anything outside the five structured shapes -- a six-field pattern, a
// month restriction, a step in the day fields, weekday names, junk -- comes back
// as `custom` with the text untouched.
func FromCron(expr string) types.Schedule {
	custom := types.Schedule{Every: types.FreqCustom, Cron: strPtr(expr)}

	fields := strings.Fields(strings.TrimSpace(expr))
	if len(fields) != 5 {
		return custom
	}
	minute, hour, dom, month, dow := fields[0], fields[1], fields[2], fields[3], fields[4]

	// Every structured shape runs in every month.
	if month != "*" {
		return custom
	}

	everyDayOfMonth := dom == "*"
	everyWeekday := dow == "*"

	if everyDayOfMonth && everyWeekday {
		// "Every minute" and "every hour at :M" are the unstepped spellings of
		// an interval of 1. Saving them rewrites the expression to the step
		// form, which is the same schedule and reads far better than "Custom".
		if hour == "*" {
			if minute == "*" {
				return types.Schedule{Every: types.FreqMinutes, Interval: intPtr(1)}
			}
			if step, ok := stepOf(minute); ok {
				return types.Schedule{Every: types.FreqMinutes, Interval: intPtr(step)}
			}
		}

		atMinute, ok := plainInt(minute)
		if !ok {
			return custom
		}

		if hour == "*" {
			return types.Schedule{
				Every: types.FreqHours, Interval: intPtr(1), Minute: intPtr(atMinute),
			}
		}

		if step, ok := stepOf(hour); ok {
			return types.Schedule{
				Every: types.FreqHours, Interval: intPtr(step), Minute: intPtr(atMinute),
			}
		}

		atHour, ok := plainInt(hour)
		if !ok {
			return custom
		}
		return types.Schedule{
			Every: types.FreqDay, Hour: intPtr(atHour), Minute: intPtr(atMinute),
		}
	}

	atMinute, minuteOK := plainInt(minute)
	atHour, hourOK := plainInt(hour)
	if !minuteOK || !hourOK {
		return custom
	}

	if everyDayOfMonth {
		weekdays, ok := parseWeekdays(dow)
		if !ok {
			return custom
		}
		return types.Schedule{
			Every:    types.FreqWeek,
			Weekdays: weekdays,
			Hour:     intPtr(atHour),
			Minute:   intPtr(atMinute),
		}
	}

	if everyWeekday {
		day, ok := plainInt(dom)
		// Cron treats day 0 as invalid; leave it to the parser to reject.
		if !ok || day < 1 || day > 31 {
			return custom
		}
		return types.Schedule{
			Every: types.FreqMonth, Day: intPtr(day), Hour: intPtr(atHour), Minute: intPtr(atMinute),
		}
	}

	// Both day fields constrained: cron ORs them, which no single control shows.
	return custom
}

func intPtr(value int) *int { return &value }

func strPtr(value string) *string { return &value }
