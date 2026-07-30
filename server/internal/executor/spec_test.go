package executor

import (
	"testing"

	"breckr-server/internal/types"
)

/*
Specs arrive over HTTP from a form, so validation is the only thing standing
between a typo and a monitor that silently never fires. Every case here must be
rejected with a message that says what to fix, and name the field so the
dashboard can point at it.
*/

func validSpec() *types.TaskSpec {
	return &types.TaskSpec{
		URL: "https://example.com/prices",
		Conditions: []types.Condition{{
			Selector: ".price",
			Extract:  types.ExtractNumber,
			Operator: types.OpLT,
			Value:    "100",
		}},
	}
}

// with applies overrides to a copy of the valid spec.
func with(mutate func(s *types.TaskSpec)) *types.TaskSpec {
	spec := validSpec()
	mutate(spec)
	return spec
}

// withCondition applies overrides to the valid spec's only condition, which is
// where most of the rejection table lives.
func withCondition(mutate func(c *types.Condition)) *types.TaskSpec {
	spec := validSpec()
	mutate(&spec.Conditions[0])
	return spec
}

func TestValidateSpecAcceptsAndNormalizes(t *testing.T) {
	t.Run("a minimal spec", func(t *testing.T) {
		spec, err := ValidateSpec(validSpec())
		if err != nil {
			t.Fatalf("ValidateSpec: %v", err)
		}

		if len(spec.Conditions) != 1 {
			t.Fatalf("unexpected spec: %+v", spec)
		}
		if spec.Conditions[0].Selector != ".price" || spec.Conditions[0].Operator != types.OpLT {
			t.Fatalf("unexpected spec: %+v", spec)
		}
		if spec.Conditions[0].WaitForSelector != "" || spec.Message != "" {
			t.Fatalf("absent optionals must stay absent: %+v", spec)
		}
	})

	t.Run("an unstated match mode defaults to all", func(t *testing.T) {
		// Every spec stored before conditions became a list says nothing, and
		// each had exactly one condition -- so "all" has to be what silence means.
		spec, err := ValidateSpec(with(func(s *types.TaskSpec) { s.Match = "" }))
		if err != nil {
			t.Fatalf("ValidateSpec: %v", err)
		}

		if spec.Match != types.MatchAll {
			t.Fatalf("match = %q, want %q", spec.Match, types.MatchAll)
		}
	})

	t.Run("blank optionals are dropped rather than stored", func(t *testing.T) {
		spec, err := ValidateSpec(with(func(s *types.TaskSpec) {
			s.Conditions[0].WaitForSelector = "  "
			s.Message = ""
		}))
		if err != nil {
			t.Fatalf("ValidateSpec: %v", err)
		}

		if spec.Conditions[0].WaitForSelector != "" || spec.Message != "" {
			t.Fatalf("blank optionals should collapse away: %+v", spec)
		}
	})

	t.Run("a stale attribute is dropped when the kind does not use one", func(t *testing.T) {
		// Switching `extract` in the form leaves the old attribute in the
		// payload; storing it would be misleading noise in a spec that never
		// reads it.
		spec, err := ValidateSpec(withCondition(func(c *types.Condition) { c.Attribute = "href" }))
		if err != nil {
			t.Fatalf("ValidateSpec: %v", err)
		}

		if spec.Conditions[0].Attribute != "" {
			t.Fatalf("attribute should be dropped, got %q", spec.Conditions[0].Attribute)
		}
	})

	t.Run("value-less operators need no value", func(t *testing.T) {
		spec, err := ValidateSpec(&types.TaskSpec{
			URL: "https://example.com",
			Conditions: []types.Condition{{
				Selector: "#banner",
				Extract:  types.ExtractExists,
				Operator: types.OpIsTrue,
			}},
		})
		if err != nil {
			t.Fatalf("ValidateSpec: %v", err)
		}

		if spec.Conditions[0].Value != "" {
			t.Fatalf("value should stay empty, got %q", spec.Conditions[0].Value)
		}
	})

	t.Run("every documented placeholder is accepted", func(t *testing.T) {
		_, err := ValidateSpec(with(func(s *types.TaskSpec) {
			s.Message = "{{name}}: {{value}} (raw {{raw}}) at {{url}}"
		}))
		if err != nil {
			t.Fatalf("ValidateSpec: %v", err)
		}
	})

	t.Run("an indexed placeholder within range is accepted", func(t *testing.T) {
		_, err := ValidateSpec(with(func(s *types.TaskSpec) {
			s.Conditions = append(s.Conditions, types.Condition{
				Selector: ".stock", Extract: types.ExtractText,
				Operator: types.OpEq, Value: "In stock",
			})
			s.Message = "{{value1}} / {{raw2}}"
		}))
		if err != nil {
			t.Fatalf("ValidateSpec: %v", err)
		}
	})
}

func TestValidateSpecRejections(t *testing.T) {
	cases := []struct {
		name     string
		spec     *types.TaskSpec
		field    string
		expected string
	}{
		{"a nil spec", nil, "spec", "`spec` must be an object"},
		{
			"a missing url",
			with(func(s *types.TaskSpec) { s.URL = "" }),
			"url", "`url` must be a non-empty string",
		},
		{
			"an unparseable url",
			with(func(s *types.TaskSpec) { s.URL = "not a url" }),
			"url", "is not a valid absolute URL",
		},
		{
			"the file scheme",
			with(func(s *types.TaskSpec) { s.URL = "file:///etc/passwd" }),
			"url", "must be http:// or https://",
		},
		{
			"the javascript scheme",
			with(func(s *types.TaskSpec) { s.URL = "javascript:alert(1)" }),
			"url", "must be http:// or https://",
		},
		{
			"a missing selector",
			withCondition(func(c *types.Condition) { c.Selector = "" }),
			"conditions[0].selector", "`selector` must be",
		},
		{
			"an unknown extract kind",
			withCondition(func(c *types.Condition) { c.Extract = "innerHTML" }),
			"conditions[0].extract", "`extract` must be one of",
		},
		{
			"an operator that cannot apply to the kind",
			withCondition(func(c *types.Condition) {
				c.Extract = types.ExtractExists
				c.Operator = types.OpGT
			}),
			"conditions[0].operator", `cannot be used with extract "exists"`,
		},
		{
			"the attribute kind without an attribute",
			withCondition(func(c *types.Condition) {
				c.Extract = types.ExtractAttribute
				c.Operator = types.OpEq
				c.Value = "x"
				c.Attribute = ""
			}),
			"conditions[0].attribute", "`attribute` is required",
		},
		{
			"a missing value for an operator that needs one",
			withCondition(func(c *types.Condition) { c.Value = "" }),
			"conditions[0].value", "`value` is required for operator \"lt\"",
		},
		{
			"a non-numeric value on a numeric kind",
			withCondition(func(c *types.Condition) { c.Value = "cheap" }),
			"conditions[0].value", "`value` must be a number when extract is \"number\"",
		},
		{
			"an unknown message placeholder",
			with(func(s *types.TaskSpec) { s.Message = "Price is {{prive}}" }),
			"message", "unknown placeholder {{prive}}",
		},
		{
			"no conditions at all",
			with(func(s *types.TaskSpec) { s.Conditions = nil }),
			"conditions", "at least one condition",
		},
		{
			"more conditions than the cap allows",
			with(func(s *types.TaskSpec) {
				for len(s.Conditions) <= types.MaxConditions {
					s.Conditions = append(s.Conditions, s.Conditions[0])
				}
			}),
			"conditions", "at most 10 conditions",
		},
		{
			"an unknown match mode",
			with(func(s *types.TaskSpec) { s.Match = "either" }),
			"match", "`match` must be one of",
		},
		{
			"an indexed placeholder past the last condition",
			with(func(s *types.TaskSpec) { s.Message = "Price is {{value2}}" }),
			"message", "the task has only 1 condition",
		},
		{
			// The second condition is the broken one, and the field has to say so
			// -- the first is the one row you can be sure is fine.
			"a broken condition names its own position",
			with(func(s *types.TaskSpec) {
				s.Conditions = append(s.Conditions, types.Condition{
					Selector: ".stock", Extract: types.ExtractText, Operator: types.OpEq,
				})
			}),
			"conditions[1].value", "`value` is required for operator \"eq\"",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidateSpec(tc.spec)
			assertValidationError(t, err, tc.field, tc.expected)
		})
	}
}

// --- The envelope -----------------------------------------------------------

func TestResolveCron(t *testing.T) {
	daily := &types.Schedule{Every: types.FreqDay, Hour: intPtr(9), Minute: intPtr(0)}

	t.Run("converts the dashboard's schedule into the stored cron", func(t *testing.T) {
		expr, err := ResolveCron(daily, nil)
		if err != nil {
			t.Fatalf("ResolveCron: %v", err)
		}
		if expr != "0 9 * * *" {
			t.Fatalf("ResolveCron = %q, want %q", expr, "0 9 * * *")
		}
	})

	t.Run("a schedule wins over a cron_expr sent alongside it", func(t *testing.T) {
		// A client that sends both must not be scheduled on the stale one.
		expr, err := ResolveCron(daily, strPtr("*/15 * * * *"))
		if err != nil {
			t.Fatalf("ResolveCron: %v", err)
		}
		if expr != "0 9 * * *" {
			t.Fatalf("ResolveCron = %q, want the schedule to win", expr)
		}
	})

	t.Run("falls back to a hand-written cron_expr", func(t *testing.T) {
		expr, err := ResolveCron(nil, strPtr("*/15 * * * *"))
		if err != nil {
			t.Fatalf("ResolveCron: %v", err)
		}
		if expr != "*/15 * * * *" {
			t.Fatalf("ResolveCron = %q", expr)
		}
	})

	t.Run("rejects an invalid cron_expr", func(t *testing.T) {
		_, err := ResolveCron(nil, strPtr("not a cron"))
		assertValidationError(t, err, "cron_expr", "is not a valid cron expression")
	})

	t.Run("rejects an invalid schedule", func(t *testing.T) {
		_, err := ResolveCron(&types.Schedule{Every: types.FreqDay, Hour: intPtr(24)}, nil)
		assertValidationError(t, err, "schedule", "`hour` must be a whole number between 0 and 23")
	})

	t.Run("rejects no schedule at all", func(t *testing.T) {
		_, err := ResolveCron(nil, nil)
		assertValidationError(t, err, "schedule", "A `schedule` or a `cron_expr` is required")
	})
}

func TestValidateTaskID(t *testing.T) {
	t.Run("accepts a boring id", func(t *testing.T) {
		id, err := ValidateTaskID("price-check")
		if err != nil || id != "price-check" {
			t.Fatalf("ValidateTaskID = %q, %v", id, err)
		}
	})

	t.Run("rejects a missing id", func(t *testing.T) {
		_, err := ValidateTaskID("")
		assertValidationError(t, err, "id", "`id` must be a non-empty string")
	})

	t.Run("rejects an id with spaces", func(t *testing.T) {
		// Ids appear in URLs, so keep them boring.
		_, err := ValidateTaskID("has space")
		assertValidationError(t, err, "id", "must contain only letters")
	})
}

func TestValidateNameRejectsBlank(t *testing.T) {
	_, err := ValidateName("  ")
	assertValidationError(t, err, "name", "`name` must be")
}
