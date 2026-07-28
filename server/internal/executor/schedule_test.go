package executor

import (
	"encoding/json"
	"errors"
	"reflect"
	"regexp"
	"testing"

	"breckr-server/internal/types"
	"breckr-server/internal/utils"
)

/*
The dashboard never sees a cron string, so this mapping is the only thing
keeping "every day at 09:00" in the form and `0 9 * * *` in the database
describing the same schedule.

Two properties matter more than any single case:

  - every expression this emits is one the scheduler will actually accept, and
  - FromCron undoes ToCron exactly, or a task's schedule would drift a little
    every time someone opened the form to fix a typo in its name.
*/

func minutes(interval int) *types.Schedule {
	return &types.Schedule{Every: types.FreqMinutes, Interval: intPtr(interval)}
}

func TestToCronAndRoundTrip(t *testing.T) {
	cases := []struct {
		name     string
		schedule *types.Schedule
		expected string
	}{
		{"every minute", minutes(1), "*/1 * * * *"},
		{"quarter-hourly", minutes(15), "*/15 * * * *"},
		{
			"every 2 hours at :30",
			&types.Schedule{Every: types.FreqHours, Interval: intPtr(2), Minute: intPtr(30)},
			"30 */2 * * *",
		},
		{
			"daily at 09:05",
			&types.Schedule{Every: types.FreqDay, Hour: intPtr(9), Minute: intPtr(5)},
			"5 9 * * *",
		},
		{
			"weekly on one day",
			&types.Schedule{Every: types.FreqWeek, Weekdays: []int{1}, Hour: intPtr(8), Minute: intPtr(0)},
			"0 8 * * 1",
		},
		{
			"weekly on several days",
			&types.Schedule{Every: types.FreqWeek, Weekdays: []int{1, 3, 5}, Hour: intPtr(8), Minute: intPtr(0)},
			"0 8 * * 1,3,5",
		},
		{
			"weekly on Sunday",
			&types.Schedule{Every: types.FreqWeek, Weekdays: []int{0}, Hour: intPtr(23), Minute: intPtr(59)},
			"59 23 * * 0",
		},
		{
			"monthly",
			&types.Schedule{Every: types.FreqMonth, Day: intPtr(1), Hour: intPtr(3), Minute: intPtr(0)},
			"0 3 1 * *",
		},
		{
			"custom",
			&types.Schedule{Every: types.FreqCustom, Cron: strPtr("0 9 */2 * *")},
			"0 9 */2 * *",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expr, err := ToCron(tc.schedule)
			if err != nil {
				t.Fatalf("ToCron: %v", err)
			}
			if expr != tc.expected {
				t.Fatalf("ToCron = %q, want %q", expr, tc.expected)
			}
			if !ValidateCronExpr(expr) {
				t.Fatalf("the scheduler has to accept what we emit: %q", expr)
			}

			// The round trip is the property that keeps a schedule from
			// drifting every time the form is opened and saved.
			if got := FromCron(expr); !reflect.DeepEqual(&got, tc.schedule) {
				t.Fatalf("FromCron(ToCron(%s)) = %+v, want %+v", tc.name, got, *tc.schedule)
			}
		})
	}
}

func TestToCronSortsAndDedupesWeekdays(t *testing.T) {
	expr, err := ToCron(&types.Schedule{
		Every: types.FreqWeek, Weekdays: []int{5, 1, 5}, Hour: intPtr(8), Minute: intPtr(0),
	})
	if err != nil {
		t.Fatalf("ToCron: %v", err)
	}
	if expr != "0 8 * * 1,5" {
		t.Fatalf("ToCron = %q, want %q -- one week must have one expression", expr, "0 8 * * 1,5")
	}
}

// The unstepped spellings of an interval of 1. Saving one rewrites it to the
// step form -- the same schedule, and far better than showing "Custom" for
// something the builder can plainly express.
func TestFromCronReadsUnsteppedFormsAsTheirStepForm(t *testing.T) {
	cases := []struct {
		expr     string
		expected types.Schedule
	}{
		{"* * * * *", *minutes(1)},
		{"30 * * * *", types.Schedule{Every: types.FreqHours, Interval: intPtr(1), Minute: intPtr(30)}},
	}

	for _, tc := range cases {
		t.Run(tc.expr, func(t *testing.T) {
			if got := FromCron(tc.expr); !reflect.DeepEqual(got, tc.expected) {
				t.Fatalf("FromCron(%q) = %+v, want %+v", tc.expr, got, tc.expected)
			}
		})
	}
}

func TestFromCronExpandsWeekdayLists(t *testing.T) {
	cases := []struct {
		name     string
		expr     string
		expected types.Schedule
	}{
		{
			"a weekday range",
			"0 7 * * 1-5",
			types.Schedule{
				Every: types.FreqWeek, Weekdays: []int{1, 2, 3, 4, 5}, Hour: intPtr(7), Minute: intPtr(0),
			},
		},
		{
			"mixed lists and ranges",
			"0 7 * * 1-2,5",
			types.Schedule{
				Every: types.FreqWeek, Weekdays: []int{1, 2, 5}, Hour: intPtr(7), Minute: intPtr(0),
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := FromCron(tc.expr); !reflect.DeepEqual(got, tc.expected) {
				t.Fatalf("FromCron(%q) = %+v, want %+v", tc.expr, got, tc.expected)
			}
		})
	}
}

// Everything the builder has no control for. These have to survive an edit
// untouched rather than being rounded to the nearest shape that does have one.
func TestFromCronKeepsUnrepresentableExpressionsCustom(t *testing.T) {
	cases := []struct {
		name string
		expr string
	}{
		{"six fields", "* * * * * *"},
		{"too few fields", "*/15 * * *"},
		{"a month restriction", "0 9 1 6 *"},
		{"a step in the day of month", "0 9 */2 * *"},
		{"a step in the weekday", "0 9 * * */2"},
		{"a weekday name", "0 9 * * MON"},
		{"7 for Sunday", "0 9 * * 7"},
		{"a wrapping weekday range", "0 9 * * 5-1"},
		{"both day fields constrained", "0 9 1 * 1"},
		{"an hour range", "0 9-17 * * *"},
		{"a minute list", "0,30 9 * * *"},
		{"junk", "not a cron"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expected := types.Schedule{Every: types.FreqCustom, Cron: strPtr(tc.expr)}
			if got := FromCron(tc.expr); !reflect.DeepEqual(got, expected) {
				t.Fatalf("FromCron(%q) = %+v, want it kept as custom", tc.expr, got)
			}
		})
	}
}

func TestCustomScheduleSurvivesARoundTripThroughTheForm(t *testing.T) {
	const stored = "0 9 */2 * *"

	// What the form would send back after being opened and saved unchanged.
	schedule := FromCron(stored)
	expr, err := ToCron(&schedule)
	if err != nil {
		t.Fatalf("ToCron: %v", err)
	}
	if expr != stored {
		t.Fatalf("round trip rewrote %q to %q", stored, expr)
	}
}

func TestToCronRejections(t *testing.T) {
	cases := []struct {
		name     string
		schedule *types.Schedule
		expected string
	}{
		{"a nil schedule", nil, "`schedule` must be an object"},
		{"an unknown frequency", &types.Schedule{Every: "fortnight"}, "`every` must be one of"},
		{
			"an interval below the range",
			minutes(0),
			"`interval` must be a whole number between 1 and 59",
		},
		{
			"an interval above the range",
			minutes(60),
			"`interval` must be a whole number between 1 and 59",
		},
		{
			"more than 23 hours",
			&types.Schedule{Every: types.FreqHours, Interval: intPtr(24), Minute: intPtr(0)},
			"`interval` must be a whole number between 1 and 23",
		},
		{
			"a missing number",
			&types.Schedule{Every: types.FreqDay, Hour: intPtr(9)},
			"`minute` must be a whole number between 0 and 59",
		},
		{
			"no weekdays",
			&types.Schedule{Every: types.FreqWeek, Weekdays: []int{}, Hour: intPtr(9), Minute: intPtr(0)},
			"at least one day",
		},
		{
			"a weekday out of range",
			&types.Schedule{Every: types.FreqWeek, Weekdays: []int{7}, Hour: intPtr(9), Minute: intPtr(0)},
			"`weekdays` must be a whole number between 0 and 6",
		},
		{
			"day 0 of the month",
			&types.Schedule{Every: types.FreqMonth, Day: intPtr(0), Hour: intPtr(9), Minute: intPtr(0)},
			"`day` must be a whole number between 1 and 31",
		},
		{
			"an empty custom cron",
			&types.Schedule{Every: types.FreqCustom, Cron: strPtr("  ")},
			"must be a non-empty string",
		},
		{
			"an invalid custom cron",
			&types.Schedule{Every: types.FreqCustom, Cron: strPtr("not a cron")},
			"is not a valid cron expression",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ToCron(tc.schedule)
			assertValidationError(t, err, "schedule", tc.expected)
		})
	}
}

// A fractional number cannot reach ToCron at all -- it is rejected while the
// body is decoded, which is the earliest and clearest place to catch it.
func TestAFractionalNumberIsRejectedWhileDecoding(t *testing.T) {
	var schedule types.Schedule
	err := json.Unmarshal([]byte(`{"every":"day","hour":9.5,"minute":0}`), &schedule)
	if err == nil {
		t.Fatal("decoding hour 9.5 into a Schedule should fail")
	}
}

// assertValidationError checks that err is a *utils.ValidationError blaming the
// expected field. The form renders the message against that control, so naming
// the wrong one is a real bug rather than cosmetics.
func assertValidationError(t *testing.T, err error, field, pattern string) {
	t.Helper()

	if err == nil {
		t.Fatalf("expected a validation error matching %q", pattern)
	}

	var ve *utils.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected a *utils.ValidationError, got %T: %v", err, err)
	}
	if ve.Field != field {
		t.Fatalf("blamed field %q, want %q (message: %s)", ve.Field, field, ve.Message)
	}
	if !regexp.MustCompile(regexp.QuoteMeta(pattern)).MatchString(ve.Message) {
		t.Fatalf("message %q does not contain %q", ve.Message, pattern)
	}
}
