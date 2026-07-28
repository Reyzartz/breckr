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

// previousValue is what this task's run last returned, for `changed`.
func (e *Executor) previousValue(taskID string) any {
	if e.history == nil {
		return nil
	}

	previous, ok := e.history.GetLastSuccessfulResult(taskID).(map[string]any)
	if !ok {
		return nil
	}

	switch value := previous["value"].(type) {
	case float64, string, bool:
		return value
	default:
		return nil
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

func extractValue(page types.Page, spec *types.TaskSpec) (any, string, error) {
	switch spec.Extract {
	case types.ExtractExists:
		present, err := page.Exists(spec.Selector)
		if err != nil {
			return nil, "", err
		}
		raw := ""
		if present {
			raw = "present"
		}
		return present, raw, nil

	case types.ExtractCount:
		count, err := page.Count(spec.Selector)
		if err != nil {
			return nil, "", err
		}
		return float64(count), strconv.Itoa(count), nil

	case types.ExtractAttribute:
		// ValidateSpec guarantees Attribute whenever the kind is "attribute",
		// and specs are validated before they are ever stored.
		raw, err := page.Attribute(spec.Selector, spec.Attribute)
		if err != nil {
			return nil, "", err
		}
		return raw, raw, nil
	}

	text, err := page.Text(spec.Selector)
	if err != nil {
		return nil, "", err
	}
	raw := strings.TrimSpace(text)

	if spec.Extract == types.ExtractNumber {
		parsed, err := ParseNumber(raw, spec.Selector)
		if err != nil {
			return nil, "", err
		}
		return parsed, raw, nil
	}

	return raw, raw, nil
}

// shouldWait reports whether to wait for the selector before extracting.
//
// `exists` and `count` are the two kinds that must not wait. Waiting for a
// selector that is *expected* to be absent would burn the selector timeout on
// every run and then fail the run, which is exactly backwards for "alert me
// when this appears".
func shouldWait(spec *types.TaskSpec) bool {
	if spec.WaitForSelector != "" {
		return true
	}
	return spec.Extract != types.ExtractExists && spec.Extract != types.ExtractCount
}

// Execute runs one spec against a page.
func Execute(page types.Page, spec *types.TaskSpec) (*types.TaskResult, error) {
	if err := page.Navigate(spec.URL); err != nil {
		return nil, err
	}

	if shouldWait(spec) {
		target := spec.WaitForSelector
		if target == "" {
			target = spec.Selector
		}
		// An explicit sub-timeout, so a selector that stopped matching fails as
		// "waiting for .price" rather than as a generic run timeout that says
		// nothing about which step stalled.
		if err := page.WaitForSelector(target, types.SelectorTimeout); err != nil {
			return nil, fmt.Errorf("no element matched %q at %s: %w", target, spec.URL, err)
		}
	}

	value, raw, err := extractValue(page, spec)
	if err != nil {
		return nil, err
	}

	return &types.TaskResult{
		Value:     value,
		Raw:       raw,
		URL:       spec.URL,
		CheckedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}, nil
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

// EvaluateCondition applies the spec's operator to one extraction.
//
// `previous` is only consulted by `changed`; passing nil means "no successful
// run to compare against", which reads as no change -- so a task never alerts on
// the very first thing it sees.
func EvaluateCondition(spec *types.TaskSpec, result *types.TaskResult, previous any) bool {
	value := result.Value

	switch spec.Operator {
	case types.OpIsTrue:
		return value == true
	case types.OpIsFalse:
		return value == false
	case types.OpChanged:
		return previous != nil && previous != value
	case types.OpLT:
		return toNumber(value) < toNumber(spec.Value)
	case types.OpLTE:
		return toNumber(value) <= toNumber(spec.Value)
	case types.OpGT:
		return toNumber(value) > toNumber(spec.Value)
	case types.OpGTE:
		return toNumber(value) >= toNumber(spec.Value)
	case types.OpContains:
		return strings.Contains(stringify(value), spec.Value)
	case types.OpNotContains:
		return !strings.Contains(stringify(value), spec.Value)
	case types.OpEq:
		// Compared as strings so "10" from the page matches 10 from the form --
		// numeric kinds are already numbers, and everything else is text anyway.
		return stringify(value) == spec.Value
	case types.OpNeq:
		return stringify(value) != spec.Value
	}

	return false
}

// RenderMessage renders the alert body by substitution. Never evaluated as code.
func RenderMessage(spec *types.TaskSpec, result *types.TaskResult, taskName string) string {
	if spec.Message == "" {
		return fmt.Sprintf("Task %q matched: %s (%s)", taskName, stringify(result.Value), spec.URL)
	}

	values := map[string]string{
		"value": stringify(result.Value),
		"raw":   result.Raw,
		"url":   result.URL,
		"name":  taskName,
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
			return EvaluateCondition(spec, result, e.previousValue(task.ID)), nil
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

	conditionMet := EvaluateCondition(spec, result, nil)

	return result, conditionMet, RenderMessage(spec, result, taskName), nil
}
