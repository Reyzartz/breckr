package executor

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"breckr-server/internal/types"
)

/*
The condition and message half of the executor -- everything except the page.

A spec is interpreted, not evaluated, so this is where "does `lt` actually mean
less-than" is pinned down. No browser and no database are involved.
*/

func conditionSpec(mutate func(s *types.TaskSpec)) *types.TaskSpec {
	spec := &types.TaskSpec{
		URL:      "https://example.com",
		Selector: ".price",
		Extract:  types.ExtractNumber,
		Operator: types.OpLT,
	}
	mutate(spec)
	return spec
}

func resultOf(value any) *types.TaskResult {
	return resultOfRaw(value, stringify(value))
}

func resultOfRaw(value any, raw string) *types.TaskResult {
	return &types.TaskResult{
		Value:     value,
		Raw:       raw,
		URL:       "https://example.com",
		CheckedAt: "2026-01-01T00:00:00Z",
	}
}

func TestEvaluateConditionNumericOperators(t *testing.T) {
	cases := []struct {
		operator types.CompareOperator
		value    string
		at       float64
		expected bool
	}{
		{types.OpLT, "100", 99, true},
		{types.OpLT, "100", 100, false},
		{types.OpLTE, "100", 100, true},
		{types.OpGT, "100", 101, true},
		{types.OpGT, "100", 100, false},
		{types.OpGTE, "100", 100, true},
		{types.OpEq, "100", 100, true},
		{types.OpEq, "100", 101, false},
		{types.OpNeq, "100", 101, true},
	}

	for _, tc := range cases {
		name := fmt.Sprintf("%s %s against %v is %t", tc.operator, tc.value, tc.at, tc.expected)
		t.Run(name, func(t *testing.T) {
			spec := conditionSpec(func(s *types.TaskSpec) {
				s.Operator = tc.operator
				s.Value = tc.value
			})
			if got := EvaluateCondition(spec, resultOf(tc.at), nil); got != tc.expected {
				t.Fatalf("EvaluateCondition = %t, want %t", got, tc.expected)
			}
		})
	}
}

func TestEvaluateConditionTextualOperators(t *testing.T) {
	cases := []struct {
		operator types.CompareOperator
		value    string
		at       string
		expected bool
	}{
		{types.OpContains, "stock", "In stock now", true},
		{types.OpContains, "stock", "Sold out", false},
		{types.OpNotContains, "Sold out", "In stock now", true},
		{types.OpNotContains, "Sold out", "Sold out", false},
		{types.OpEq, "In stock", "In stock", true},
		{types.OpNeq, "In stock", "Sold out", true},
	}

	for _, tc := range cases {
		name := fmt.Sprintf("%s %q against %q is %t", tc.operator, tc.value, tc.at, tc.expected)
		t.Run(name, func(t *testing.T) {
			spec := conditionSpec(func(s *types.TaskSpec) {
				s.Extract = types.ExtractText
				s.Operator = tc.operator
				s.Value = tc.value
			})
			if got := EvaluateCondition(spec, resultOf(tc.at), nil); got != tc.expected {
				t.Fatalf("EvaluateCondition = %t, want %t", got, tc.expected)
			}
		})
	}
}

func TestIsTrueAndIsFalseReadAnExistenceCheck(t *testing.T) {
	present := conditionSpec(func(s *types.TaskSpec) {
		s.Extract = types.ExtractExists
		s.Operator = types.OpIsTrue
	})
	absent := conditionSpec(func(s *types.TaskSpec) {
		s.Extract = types.ExtractExists
		s.Operator = types.OpIsFalse
	})

	cases := []struct {
		spec     *types.TaskSpec
		value    bool
		expected bool
	}{
		{present, true, true},
		{present, false, false},
		{absent, false, true},
		{absent, true, false},
	}

	for _, tc := range cases {
		if got := EvaluateCondition(tc.spec, resultOf(tc.value), nil); got != tc.expected {
			t.Fatalf("%s against %t = %t, want %t", tc.spec.Operator, tc.value, got, tc.expected)
		}
	}
}

func TestEqComparesAsStrings(t *testing.T) {
	// The form always yields a string; a `number` extraction always yields a
	// number. Comparing them by type would make eq permanently false.
	spec := conditionSpec(func(s *types.TaskSpec) {
		s.Operator = types.OpEq
		s.Value = "10"
	})

	if !EvaluateCondition(spec, resultOf(float64(10)), nil) {
		t.Fatal("a page number must match the form's string")
	}
}

// --- changed ----------------------------------------------------------------

func TestChangedOperator(t *testing.T) {
	changed := conditionSpec(func(s *types.TaskSpec) {
		s.Extract = types.ExtractText
		s.Operator = types.OpChanged
	})

	t.Run("is false on the very first run", func(t *testing.T) {
		// Nothing to compare against, so a brand-new task must not alert on
		// whatever it happens to see first.
		if EvaluateCondition(changed, resultOf("18:25:43"), nil) {
			t.Fatal("changed fired with no previous run")
		}
	})

	t.Run("fires when the value differs from the last success", func(t *testing.T) {
		if !EvaluateCondition(changed, resultOf("18:25:44"), "18:25:43") {
			t.Fatal("changed did not fire on a differing value")
		}
	})

	t.Run("is false when the value held steady", func(t *testing.T) {
		// This is what re-arms the edge-trigger: the run after a change sees no
		// change, so the next change fires again.
		if EvaluateCondition(changed, resultOf("18:25:43"), "18:25:43") {
			t.Fatal("changed fired on an unchanged value")
		}
	})
}

// --- messages ---------------------------------------------------------------

func TestRenderMessage(t *testing.T) {
	t.Run("renders every placeholder", func(t *testing.T) {
		spec := conditionSpec(func(s *types.TaskSpec) {
			s.Message = "{{name}}: {{value}} (raw {{raw}}) at {{url}}"
		})

		got := RenderMessage(spec, resultOfRaw(float64(42), "$42.00"), "Price check")
		want := "Price check: 42 (raw $42.00) at https://example.com"
		if got != want {
			t.Fatalf("RenderMessage = %q, want %q", got, want)
		}
	})

	t.Run("tolerates whitespace inside a placeholder", func(t *testing.T) {
		spec := conditionSpec(func(s *types.TaskSpec) { s.Message = "now {{ value }}" })

		if got := RenderMessage(spec, resultOf(float64(7)), "T"); got != "now 7" {
			t.Fatalf("RenderMessage = %q, want %q", got, "now 7")
		}
	})

	t.Run("falls back to a default body when no template is set", func(t *testing.T) {
		got := RenderMessage(conditionSpec(func(*types.TaskSpec) {}), resultOf(float64(42)), "Price check")

		if !strings.Contains(got, "Price check") || !strings.Contains(got, "42") {
			t.Fatalf("default body %q should name the task and the value", got)
		}
	})

	t.Run("a template is substituted, never evaluated", func(t *testing.T) {
		// The whole point of the declarative spec: no user string reaches an
		// interpreter, so this stays literal text.
		spec := conditionSpec(func(s *types.TaskSpec) {
			s.Message = "${process.exit(1)} and {{value}}"
		})

		got := RenderMessage(spec, resultOf(float64(1)), "T")
		if got != "${process.exit(1)} and 1" {
			t.Fatalf("RenderMessage = %q -- the template must be substituted, not evaluated", got)
		}
	})
}

// --- extraction -------------------------------------------------------------

// fakePage is a types.Page that answers from a fixture, so extraction is
// testable without a browser.
type fakePage struct {
	visited   string
	text      string
	attribute string
	count     int
	exists    bool
	err       error
}

func (f *fakePage) Navigate(url string) error { f.visited = url; return f.err }
func (f *fakePage) WaitForSelector(string, time.Duration) error {
	return f.err
}
func (f *fakePage) Exists(string) (bool, error) { return f.exists, f.err }
func (f *fakePage) Count(string) (int, error)   { return f.count, f.err }
func (f *fakePage) Attribute(string, string) (string, error) {
	return f.attribute, f.err
}
func (f *fakePage) Text(string) (string, error) { return f.text, f.err }

func TestExecuteExtraction(t *testing.T) {
	cases := []struct {
		name      string
		extract   types.ExtractKind
		page      *fakePage
		wantValue any
		wantRaw   string
	}{
		{"text trims", types.ExtractText, &fakePage{text: "  In stock  "}, "In stock", "In stock"},
		{
			"number strips currency and separators",
			types.ExtractNumber,
			&fakePage{text: "$1,299.00"},
			1299.0, "$1,299.00",
		},
		{"count", types.ExtractCount, &fakePage{count: 3}, 3.0, "3"},
		{"exists present", types.ExtractExists, &fakePage{exists: true}, true, "present"},
		{"exists absent", types.ExtractExists, &fakePage{exists: false}, false, ""},
		{
			"attribute",
			types.ExtractAttribute,
			&fakePage{attribute: "/deal"},
			"/deal", "/deal",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := conditionSpec(func(s *types.TaskSpec) {
				s.Extract = tc.extract
				s.Attribute = "href"
				s.Operator = types.OpChanged
			})

			result, err := Execute(tc.page, spec)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if result.Value != tc.wantValue {
				t.Fatalf("value = %#v, want %#v", result.Value, tc.wantValue)
			}
			if result.Raw != tc.wantRaw {
				t.Fatalf("raw = %q, want %q", result.Raw, tc.wantRaw)
			}
			if tc.page.visited != spec.URL {
				t.Fatalf("navigated to %q, want %q", tc.page.visited, spec.URL)
			}
		})
	}
}

// A number that will not parse fails the run rather than becoming NaN. NaN
// compares false against every threshold, so the monitor would look healthy and
// never fire -- exactly the failure this app exists to avoid.
func TestNumberExtractionFailsRatherThanYieldingNaN(t *testing.T) {
	spec := conditionSpec(func(s *types.TaskSpec) {
		s.Extract = types.ExtractNumber
		s.Value = "100"
	})

	if _, err := Execute(&fakePage{text: "Sold out"}, spec); err == nil {
		t.Fatal("an unparseable number must fail the run")
	}
}
