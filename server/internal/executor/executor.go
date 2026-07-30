// Package executor turns a declarative spec into something the runner can
// execute.
//
// The spec is *interpreted*, never evaluated -- no user string is ever compiled
// or handed to a script engine. That is what makes it safe to author a task
// from a dashboard that has no authentication in front of it.
package executor

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"breckr-server/internal/types"
)

// PreviousValueLookup is how the `changed` operator reaches run history.
//
// A narrow interface rather than the run store itself, so the executor stays
// free of a store dependency -- and so the operator table is testable with a
// two-line fake.
type PreviousValueLookup interface {
	GetLastSuccessfulResult(taskID string) any
}

// Executor compiles stored tasks. It holds only the history lookup the
// `changed` operator needs.
type Executor struct {
	history PreviousValueLookup
	timeout time.Duration
}

func New(history PreviousValueLookup, timeout time.Duration) *Executor {
	return &Executor{history: history, timeout: timeout}
}

// previousValues is what this task's run last returned, keyed by condition, for
// `changed`.
//
// Keyed rather than indexed so a condition still finds its own previous value
// after the list has been reordered -- see types.Condition.Key. A condition
// whose key is absent reads as "no previous", which `changed` treats as no
// change, so the worst an edit can cost is one skipped alert.
func (e *Executor) previousValues(taskID string, spec *types.TaskSpec) map[string]any {
	if e.history == nil {
		return nil
	}

	previous, ok := e.history.GetLastSuccessfulResult(taskID).(map[string]any)
	if !ok {
		return nil
	}

	values := make(map[string]any)

	if checks, ok := previous["checks"].([]any); ok {
		for _, entry := range checks {
			check, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			key, ok := check["key"].(string)
			if !ok {
				continue
			}
			if value, ok := comparableValue(check["value"]); ok {
				values[key] = value
			}
		}
		return values
	}

	// A result stored before conditions became a list carries one bare value. It
	// belongs to whichever condition the flat spec was hoisted into, which is
	// always the first -- so a task that has been watching a page for months
	// keeps its history the moment a second condition is added beside the
	// original, rather than going quiet for one run.
	if len(spec.Conditions) > 0 {
		if value, ok := comparableValue(previous["value"]); ok {
			values[spec.Conditions[0].Key()] = value
		}
	}

	return values
}

// comparableValue keeps only the types `changed` can compare with !=.
//
// Anything else -- an object, an array, a null -- would either panic the
// comparison or compare unequal every time, and both are worse than reporting
// no previous value at all.
func comparableValue(raw any) (any, bool) {
	switch value := raw.(type) {
	case float64, string, bool:
		return value, true
	default:
		return nil, false
	}
}

var numberJunk = regexp.MustCompile(`[^0-9.-]`)

// ParseNumber pulls a number out of whatever the page rendered -- "$1,299.00",
// "1 299 kr".
//
// Errors rather than returning NaN: a selector that started matching a
// different element would otherwise compare NaN against the threshold, which is
// false for every operator. The monitor would look healthy and never fire.
func ParseNumber(raw, selector string) (float64, error) {
	cleaned := numberJunk.ReplaceAllString(raw, "")

	parsed, err := strconv.ParseFloat(cleaned, 64)
	if err != nil {
		return 0, fmt.Errorf("could not parse a number from %q at %q", raw, selector)
	}
	return parsed, nil
}

func extractValue(page types.Page, condition *types.Condition) (any, string, error) {
	switch condition.Extract {
	case types.ExtractExists:
		present, err := page.Exists(condition.Selector)
		if err != nil {
			return nil, "", err
		}
		raw := ""
		if present {
			raw = "present"
		}
		return present, raw, nil

	case types.ExtractCount:
		count, err := page.Count(condition.Selector)
		if err != nil {
			return nil, "", err
		}
		return float64(count), strconv.Itoa(count), nil

	case types.ExtractAttribute:
		// ValidateSpec guarantees Attribute whenever the kind is "attribute",
		// and specs are validated before they are ever stored.
		raw, err := page.Attribute(condition.Selector, condition.Attribute)
		if err != nil {
			return nil, "", err
		}
		return raw, raw, nil
	}

	text, err := page.Text(condition.Selector)
	if err != nil {
		return nil, "", err
	}
	raw := strings.TrimSpace(text)

	if condition.Extract == types.ExtractNumber {
		parsed, err := ParseNumber(raw, condition.Selector)
		if err != nil {
			return nil, "", err
		}
		return parsed, raw, nil
	}

	return raw, raw, nil
}

// shouldWait reports whether to wait for this condition's selector before
// extracting.
//
// `exists` and `count` are the two kinds that must not wait. Waiting for a
// selector that is *expected* to be absent would burn the selector timeout on
// every run and then fail the run, which is exactly backwards for "alert me
// when this appears".
func shouldWait(condition *types.Condition) bool {
	if condition.WaitForSelector != "" {
		return true
	}
	return condition.Extract != types.ExtractExists && condition.Extract != types.ExtractCount
}

// Execute runs one spec against a page: one navigation, then every condition in
// order.
//
// An extraction that fails fails the whole run rather than counting as "not
// met". A selector that stopped matching is the most common way a monitor
// breaks, and the run row is where that has to be visible -- folding it into a
// quiet false is how a task ends up never firing and nobody noticing.
func Execute(page types.Page, spec *types.TaskSpec) (*types.TaskResult, error) {
	if err := page.Navigate(spec.URL); err != nil {
		return nil, err
	}

	checks := make([]types.CheckResult, 0, len(spec.Conditions))

	for i := range spec.Conditions {
		condition := &spec.Conditions[i]

		if shouldWait(condition) {
			target := condition.WaitForSelector
			if target == "" {
				target = condition.Selector
			}
			// An explicit sub-timeout, so a selector that stopped matching fails
			// as "waiting for .price" rather than as a generic run timeout that
			// says nothing about which step stalled.
			if err := page.WaitForSelector(target, types.SelectorTimeout); err != nil {
				return nil, fmt.Errorf("no element matched %q at %s: %w", target, spec.URL, err)
			}
		}

		value, raw, err := extractValue(page, condition)
		if err != nil {
			return nil, err
		}

		checks = append(checks, types.CheckResult{
			Key:   condition.Key(),
			Value: value,
			Raw:   raw,
		})
	}

	result := &types.TaskResult{
		URL:       spec.URL,
		CheckedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Checks:    checks,
	}

	// The first condition is also the top-level value, so {{value}} and every
	// stored result keep the shape they had when a task had exactly one.
	if len(checks) > 0 {
		result.Value = checks[0].Value
		result.Raw = checks[0].Raw
	}

	return result, nil
}

// stringify renders an extracted value the way the TypeScript executor did, so
// eq/neq and {{value}} keep comparing and printing identically.
//
// Numbers arrive as float64 (from the page, or decoded from stored JSON) and
// must print as "10", not "1E+01" -- 'f' with precision -1 gives the shortest
// representation that round-trips, which is what JavaScript's String(number)
// produces for every value this app sees.
func stringify(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case bool:
		return strconv.FormatBool(typed)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case int:
		return strconv.Itoa(typed)
	default:
		return fmt.Sprint(typed)
	}
}

// toNumber coerces for the ordering operators, mirroring JavaScript's Number().
// A value that will not coerce yields NaN, and every comparison against NaN is
// false -- same as before.
func toNumber(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case int:
		return float64(typed)
	case bool:
		if typed {
			return 1
		}
		return 0
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil {
			return math.NaN()
		}
		return parsed
	default:
		return math.NaN()
	}
}

// EvaluateConditions applies every condition's operator and combines the
// outcomes the way the spec's match mode says.
//
// Annotates result.Checks[i].Met on the way through. The runner stores the
// result *after* calling this, so recording each outcome here is what makes
// "which one matched" answerable from run history -- and no later step knows
// it. Nothing short-circuits for the same reason: `all` could stop at the first
// false, but then the run row would claim the rest were never checked.
func EvaluateConditions(spec *types.TaskSpec, result *types.TaskResult, previous map[string]any) bool {
	// Execute builds one check per condition, so a mismatch means a result
	// assembled by hand. False is the safe answer: a task that cannot say
	// whether it matched must not claim it did.
	if len(spec.Conditions) == 0 || len(result.Checks) != len(spec.Conditions) {
		return false
	}

	// `all` starts true and is narrowed; `any` starts false and is widened.
	met := spec.Match != types.MatchAny

	for i := range spec.Conditions {
		check := &result.Checks[i]
		check.Met = EvaluateCondition(&spec.Conditions[i], check.Value, previous[check.Key])

		if spec.Match == types.MatchAny {
			met = met || check.Met
		} else {
			met = met && check.Met
		}
	}

	return met
}

// EvaluateCondition applies one condition's operator to one extraction.
//
// `previous` is only consulted by `changed`; passing nil means "no successful
// run to compare against", which reads as no change -- so a task never alerts on
// the very first thing it sees.
func EvaluateCondition(condition *types.Condition, value any, previous any) bool {
	switch condition.Operator {
	case types.OpIsTrue:
		return value == true
	case types.OpIsFalse:
		return value == false
	case types.OpChanged:
		return previous != nil && previous != value
	case types.OpLT:
		return toNumber(value) < toNumber(condition.Value)
	case types.OpLTE:
		return toNumber(value) <= toNumber(condition.Value)
	case types.OpGT:
		return toNumber(value) > toNumber(condition.Value)
	case types.OpGTE:
		return toNumber(value) >= toNumber(condition.Value)
	case types.OpContains:
		return strings.Contains(stringify(value), condition.Value)
	case types.OpNotContains:
		return !strings.Contains(stringify(value), condition.Value)
	case types.OpEq:
		// Compared as strings so "10" from the page matches 10 from the form --
		// numeric kinds are already numbers, and everything else is text anyway.
		return stringify(value) == condition.Value
	case types.OpNeq:
		return stringify(value) != condition.Value
	}

	return false
}

// summarize lists what every condition saw, for the default alert body.
//
// Identical to the old wording for a one-condition task, which is still the
// common case -- a task with several is the one that needs all of them named.
func summarize(result *types.TaskResult) string {
	if len(result.Checks) == 0 {
		return stringify(result.Value)
	}

	parts := make([]string, len(result.Checks))
	for i, check := range result.Checks {
		parts[i] = stringify(check.Value)
	}
	return strings.Join(parts, ", ")
}

// RenderMessage renders the alert body by substitution. Never evaluated as code.
//
// {{value}} and {{raw}} stay the first condition's, so a message written when
// the task had one condition keeps saying what it said. {{value1}}…{{valueN}}
// address them individually; ValidateSpec has already rejected an index the
// task has no condition for.
func RenderMessage(spec *types.TaskSpec, result *types.TaskResult, taskName string) string {
	if spec.Message == "" {
		return fmt.Sprintf("Task %q matched: %s (%s)", taskName, summarize(result), spec.URL)
	}

	values := map[string]string{
		"value": stringify(result.Value),
		"raw":   result.Raw,
		"url":   result.URL,
		"name":  taskName,
	}

	for i, check := range result.Checks {
		values[fmt.Sprintf("value%d", i+1)] = stringify(check.Value)
		values[fmt.Sprintf("raw%d", i+1)] = check.Raw
	}

	return types.MessagePlaceholderPattern.ReplaceAllStringFunc(spec.Message, func(whole string) string {
		name := types.MessagePlaceholderPattern.FindStringSubmatch(whole)[1]
		if replacement, ok := values[name]; ok {
			return replacement
		}
		return whole
	})
}

// CompilableTask is the stored shape Compile consumes.
type CompilableTask struct {
	ID       string
	Name     string
	CronExpr string
	Spec     *types.TaskSpec
}

// Compile turns a stored task into the shape the runner consumes.
func (e *Executor) Compile(task CompilableTask) *types.ResolvedTask {
	spec := task.Spec

	return &types.ResolvedTask{
		ID:      task.ID,
		Name:    task.Name,
		Cron:    task.CronExpr,
		Timeout: e.timeout,
		// Every declarative spec reads a page, so the CDP connection is always
		// needed. The browserless path stays in the browser package for tests.
		NeedsBrowser: true,
		Run: func(page types.Page) (*types.TaskResult, error) {
			return Execute(page, spec)
		},
		Condition: func(result *types.TaskResult) (bool, error) {
			return EvaluateConditions(spec, result, e.previousValues(task.ID, spec)), nil
		},
		Notify: func(result *types.TaskResult) string {
			return RenderMessage(spec, result, task.Name)
		},
	}
}

// TestSpec runs a draft spec once, for the dashboard's "Test" button.
//
// Deliberately writes no run row and sends no notification: pressing Test while
// getting a selector right must not pollute history, and must not alert anyone.
// The `changed` operator has nothing to compare against here, so it reads false.
func TestSpec(page types.Page, spec *types.TaskSpec, taskName string) (*types.TaskResult, bool, string, error) {
	result, err := Execute(page, spec)
	if err != nil {
		return nil, false, "", err
	}

	conditionMet := EvaluateConditions(spec, result, nil)

	return result, conditionMet, RenderMessage(spec, result, taskName), nil
}
