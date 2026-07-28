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
		URL:      "https://example.com/prices",
		Selector: ".price",
		Extract:  types.ExtractNumber,
		Operator: types.OpLT,
		Value:    "100",
	}
}

// with applies overrides to a copy of the valid spec.
func with(mutate func(s *types.TaskSpec)) *types.TaskSpec {
	spec := validSpec()
	mutate(spec)
	return spec
}

func TestValidateSpecAcceptsAndNormalizes(t *testing.T) {
	t.Run("a minimal spec", func(t *testing.T) {
		spec, err := ValidateSpec(validSpec())
		if err != nil {
			t.Fatalf("ValidateSpec: %v", err)
		}

		if spec.Selector != ".price" || spec.Operator != types.OpLT {
			t.Fatalf("unexpected spec: %+v", spec)
		}
		if spec.WaitForSelector != "" || spec.Message != "" {
			t.Fatalf("absent optionals must stay absent: %+v", spec)
		}
	})

	t.Run("blank optionals are dropped rather than stored", func(t *testing.T) {
		spec, err := ValidateSpec(with(func(s *types.TaskSpec) {
			s.WaitForSelector = "  "
			s.Message = ""
		}))
		if err != nil {
			t.Fatalf("ValidateSpec: %v", err)
		}

		if spec.WaitForSelector != "" || spec.Message != "" {
			t.Fatalf("blank optionals should collapse away: %+v", spec)
		}
	})

	t.Run("a stale attribute is dropped when the kind does not use one", func(t *testing.T) {
		// Switching `extract` in the form leaves the old attribute in the
		// payload; storing it would be misleading noise in a spec that never
		// reads it.
		spec, err := ValidateSpec(with(func(s *types.TaskSpec) { s.Attribute = "href" }))
		if err != nil {
			t.Fatalf("ValidateSpec: %v", err)
		}

		if spec.Attribute != "" {
			t.Fatalf("attribute should be dropped, got %q", spec.Attribute)
		}
	})

	t.Run("value-less operators need no value", func(t *testing.T) {
		spec, err := ValidateSpec(&types.TaskSpec{
			URL:      "https://example.com",
			Selector: "#banner",
			Extract:  types.ExtractExists,
			Operator: types.OpIsTrue,
		})
		if err != nil {
			t.Fatalf("ValidateSpec: %v", err)
		}

		if spec.Value != "" {
			t.Fatalf("value should stay empty, got %q", spec.Value)
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
			with(func(s *types.TaskSpec) { s.Selector = "" }),
			"selector", "`selector` must be",
		},
		{
			"an unknown extract kind",
			with(func(s *types.TaskSpec) { s.Extract = "innerHTML" }),
			"extract", "`extract` must be one of",
		},
		{
			"an operator that cannot apply to the kind",
			with(func(s *types.TaskSpec) {
				s.Extract = types.ExtractExists
				s.Operator = types.OpGT
			}),
			"operator", `cannot be used with extract "exists"`,
		},
		{
			"the attribute kind without an attribute",
			with(func(s *types.TaskSpec) {
				s.Extract = types.ExtractAttribute
				s.Operator = types.OpEq
				s.Value = "x"
				s.Attribute = ""
			}),
			"attribute", "`attribute` is required",
		},
		{
			"a missing value for an operator that needs one",
			with(func(s *types.TaskSpec) { s.Value = "" }),
			"value", "`value` is required for operator \"lt\"",
		},
		{
			"a non-numeric value on a numeric kind",
			with(func(s *types.TaskSpec) { s.Value = "cheap" }),
			"value", "`value` must be a number when extract is \"number\"",
		},
		{
			"an unknown message placeholder",
			with(func(s *types.TaskSpec) { s.Message = "Price is {{prive}}" }),
			"message", "unknown placeholder {{prive}}",
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
