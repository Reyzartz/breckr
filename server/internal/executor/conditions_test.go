package executor

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"breckr-server/internal/types"
)

/*
The half of the executor that only exists because a task can watch more than one
thing at a time: how conditions combine, how each one finds its own history, and
how a spec written when there was only ever one still reads back.

The combining rule is the load-bearing decision here -- it is what the whole
edge-trigger state machine downstream is handed -- so all four corners of both
modes are pinned, not just the interesting ones.
*/

// twoConditions watches a price and a stock label on one page.
func twoConditions(match types.MatchMode) *types.TaskSpec {
	return &types.TaskSpec{
		URL:   "https://example.com",
		Match: match,
		Conditions: []types.Condition{
			{Selector: ".price", Extract: types.ExtractNumber, Operator: types.OpLT, Value: "100"},
			{Selector: ".stock", Extract: types.ExtractText, Operator: types.OpEq, Value: "In stock"},
		},
	}
}

// --- combining --------------------------------------------------------------

func TestAllNeedsEveryConditionAndAnyNeedsOne(t *testing.T) {
	cases := []struct {
		match    types.MatchMode
		price    float64
		stock    string
		expected bool
	}{
		{types.MatchAll, 99, "In stock", true},
		{types.MatchAll, 99, "Sold out", false},
		{types.MatchAll, 150, "In stock", false},
		{types.MatchAll, 150, "Sold out", false},
		{types.MatchAny, 99, "In stock", true},
		{types.MatchAny, 99, "Sold out", true},
		{types.MatchAny, 150, "In stock", true},
		{types.MatchAny, 150, "Sold out", false},
	}

	for _, tc := range cases {
		name := fmt.Sprintf("%s of price %v and stock %q is %t", tc.match, tc.price, tc.stock, tc.expected)
		t.Run(name, func(t *testing.T) {
			spec := twoConditions(tc.match)
			got := EvaluateConditions(spec, resultOf(spec, tc.price, tc.stock), nil)
			if got != tc.expected {
				t.Fatalf("EvaluateConditions = %t, want %t", got, tc.expected)
			}
		})
	}
}

// An empty match mode is what every spec stored before conditions became a list
// says, and each of those had exactly one condition -- so silence has to behave
// like `all` rather than like `any`, which would invert them.
func TestAnUnstatedMatchModeCombinesLikeAll(t *testing.T) {
	spec := twoConditions("")

	if EvaluateConditions(spec, resultOf(spec, 99.0, "Sold out"), nil) {
		t.Fatal("an unstated mode must require every condition, not any")
	}
	if !EvaluateConditions(spec, resultOf(spec, 99.0, "In stock"), nil) {
		t.Fatal("an unstated mode must still match when every condition does")
	}
}

// Nothing short-circuits. `all` could stop at the first false, but the run row
// is where "which one broke" gets answered, and a condition that was never
// evaluated would read there as one that did not match.
func TestEveryConditionIsRecordedEvenOnceTheAnswerIsKnown(t *testing.T) {
	spec := twoConditions(types.MatchAll)
	result := resultOf(spec, 150.0, "In stock")

	if EvaluateConditions(spec, result, nil) {
		t.Fatal("all with a failing condition must not match")
	}
	if result.Checks[0].Met {
		t.Fatal("the price condition did not match and must say so")
	}
	if !result.Checks[1].Met {
		t.Fatal("the stock condition matched and must be recorded, not skipped")
	}
}

// A result that carries a different number of checks than the spec has
// conditions cannot be evaluated. False is the only safe answer: a task that
// cannot say whether it matched must not claim that it did.
func TestAResultThatDoesNotFitTheSpecNeverMatches(t *testing.T) {
	spec := twoConditions(types.MatchAny)
	partial := &types.TaskResult{Checks: []types.CheckResult{{Value: 99.0}}}

	if EvaluateConditions(spec, partial, nil) {
		t.Fatal("a mismatched result must not match, least of all under `any`")
	}
	if EvaluateConditions(spec, &types.TaskResult{}, nil) {
		t.Fatal("a result with no checks must not match")
	}
}

// --- per-condition history --------------------------------------------------

// fakeHistory is a PreviousValueLookup that answers from a fixture.
type fakeHistory struct{ result any }

func (f fakeHistory) GetLastSuccessfulResult(string) any { return f.result }

func storedChecks(spec *types.TaskSpec, values ...any) map[string]any {
	checks := make([]any, len(values))
	for i, value := range values {
		checks[i] = map[string]any{"key": spec.Conditions[i].Key(), "value": value}
	}
	return map[string]any{"checks": checks}
}

// The reason keys exist at all. Reordering the list must not make `changed`
// compare a price against a stock label -- which is what an index would do, and
// it would fire on every run forever without ever being wrong about anything
// you could see.
func TestChangedFindsItsOwnPreviousValueAfterAReorder(t *testing.T) {
	watched := func(first, second types.Condition) *types.TaskSpec {
		return &types.TaskSpec{
			URL:        "https://example.com",
			Match:      types.MatchAny,
			Conditions: []types.Condition{first, second},
		}
	}

	price := types.Condition{Selector: ".price", Extract: types.ExtractNumber, Operator: types.OpChanged}
	stock := types.Condition{Selector: ".stock", Extract: types.ExtractText, Operator: types.OpChanged}

	before := watched(price, stock)
	history := fakeHistory{storedChecks(before, 99.0, "In stock")}

	// Same two conditions, swapped.
	after := watched(stock, price)

	previous := New(history, time.Second).previousValues("shop", after)

	result := resultOf(after, "In stock", 89.0)
	if !EvaluateConditions(after, result, previous) {
		t.Fatal("the price changed, so the task should match")
	}
	if result.Checks[0].Met {
		t.Fatal("the stock label held steady and must not report a change")
	}
	if !result.Checks[1].Met {
		t.Fatal("the price changed and must report it")
	}
}

// A task that has been watching a page for months has run history in the old
// shape. Adding a second condition beside the original must not make the first
// one forget what it saw yesterday and go quiet for a run.
func TestAStoredResultWithNoChecksStillFeedsTheFirstCondition(t *testing.T) {
	spec := twoConditions(types.MatchAny)
	spec.Conditions[0].Operator = types.OpChanged
	spec.Conditions[0].Value = ""

	history := fakeHistory{map[string]any{"value": 99.0, "raw": "$99.00"}}
	previous := New(history, time.Second).previousValues("shop", spec)

	if got := previous[spec.Conditions[0].Key()]; got != 99.0 {
		t.Fatalf("the first condition's previous value = %#v, want 99", got)
	}
	if _, ok := previous[spec.Conditions[1].Key()]; ok {
		t.Fatal("the second condition has no history and must be told so")
	}
}

// Nothing else in the map is comparable with !=, and a value that is not would
// either panic the comparison or differ from itself on every run.
func TestOnlyComparablePreviousValuesSurvive(t *testing.T) {
	spec := twoConditions(types.MatchAll)
	history := fakeHistory{storedChecks(spec, []any{1, 2}, "In stock")}

	previous := New(history, time.Second).previousValues("shop", spec)

	if _, ok := previous[spec.Conditions[0].Key()]; ok {
		t.Fatal("a non-scalar previous value must be dropped, not stored")
	}
	if previous[spec.Conditions[1].Key()] != "In stock" {
		t.Fatal("a scalar beside it must still come through")
	}
}

// --- the pre-list shape -----------------------------------------------------

// The only migration there is. Every spec in the database is flat JSON written
// before conditions became a list, and it has to keep meaning what it meant.
func TestTheSingleConditionShapeIsHoistedOnDecode(t *testing.T) {
	stored := `{
		"url": "https://example.com/prices",
		"selector": ".price",
		"waitForSelector": ".loaded",
		"extract": "number",
		"operator": "lt",
		"value": "100",
		"message": "now {{value}}"
	}`

	var spec types.TaskSpec
	if err := json.Unmarshal([]byte(stored), &spec); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if len(spec.Conditions) != 1 {
		t.Fatalf("the flat shape should hoist into one condition, got %+v", spec)
	}

	condition := spec.Conditions[0]
	if condition.Selector != ".price" || condition.WaitForSelector != ".loaded" ||
		condition.Extract != types.ExtractNumber || condition.Operator != types.OpLT ||
		condition.Value != "100" {
		t.Fatalf("the hoisted condition lost a field: %+v", condition)
	}
	if spec.Match != types.MatchAll {
		t.Fatalf("match = %q, want a hoisted spec to combine like all", spec.Match)
	}
	if spec.Message != "now {{value}}" {
		t.Fatalf("message = %q", spec.Message)
	}

	// And it still validates, which is what makes the stored row schedulable.
	if _, err := ValidateSpec(&spec); err != nil {
		t.Fatalf("a hoisted spec must still validate: %v", err)
	}
}

// A blank selector in the old shape has to be rejected as that condition's
// problem. Reporting "a task needs at least one condition" instead would name a
// field the caller never sent and say nothing about what to fix.
func TestAnEmptyLegacyConditionIsRejectedAsACondition(t *testing.T) {
	var spec types.TaskSpec
	if err := json.Unmarshal([]byte(`{"url":"https://example.com","extract":"text","operator":"changed"}`), &spec); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	_, err := ValidateSpec(&spec)
	assertValidationError(t, err, "conditions[0].selector", "`selector` must be")
}

// Validated specs are written back in the new shape, so the flat keys do not
// linger in the database beside the list that replaced them.
func TestAValidatedSpecMarshalsOnlyTheListShape(t *testing.T) {
	spec, err := ValidateSpec(validSpec())
	if err != nil {
		t.Fatalf("ValidateSpec: %v", err)
	}

	encoded, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var wire map[string]any
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	for _, key := range []string{"selector", "extract", "operator", "value", "waitForSelector"} {
		if _, ok := wire[key]; ok {
			t.Fatalf("%q should not survive into the stored shape: %s", key, encoded)
		}
	}
	if _, ok := wire["conditions"]; !ok {
		t.Fatalf("conditions missing from the stored shape: %s", encoded)
	}
}

// --- messages ---------------------------------------------------------------

func TestIndexedPlaceholdersAddressEachCondition(t *testing.T) {
	spec := twoConditions(types.MatchAll)
	spec.Message = "{{name}}: {{value1}} and {{value2}} (raw {{raw1}}), plus {{value}}"

	result := resultOfRaw(spec, []any{89.99, "In stock"}, []string{"$89.99", "In stock"})

	got := RenderMessage(spec, result, "Shop")
	want := "Shop: 89.99 and In stock (raw $89.99), plus 89.99"
	if got != want {
		t.Fatalf("RenderMessage = %q, want %q", got, want)
	}
}

// With several conditions the default body has to name all of them -- the first
// one alone would be an alert that does not say why it arrived.
func TestTheDefaultBodyListsEveryValue(t *testing.T) {
	spec := twoConditions(types.MatchAll)

	got := RenderMessage(spec, resultOf(spec, 89.99, "In stock"), "Shop")
	want := `Task "Shop" matched: 89.99, In stock (https://example.com)`
	if got != want {
		t.Fatalf("RenderMessage = %q, want %q", got, want)
	}
}

// --- extraction -------------------------------------------------------------

// selectorPage answers per selector, so one page can carry both conditions.
type selectorPage struct {
	navigations int
	waited      []string
	text        map[string]string
}

func (p *selectorPage) Navigate(string) error { p.navigations++; return nil }
func (p *selectorPage) WaitForSelector(selector string, _ time.Duration) error {
	p.waited = append(p.waited, selector)
	return nil
}
func (p *selectorPage) Exists(selector string) (bool, error) {
	_, ok := p.text[selector]
	return ok, nil
}
func (p *selectorPage) Count(string) (int, error)                { return len(p.text), nil }
func (p *selectorPage) Attribute(string, string) (string, error) { return "", nil }
func (p *selectorPage) Text(selector string) (string, error)     { return p.text[selector], nil }

// Every condition reads the same page, so the run costs one navigation however
// many things it watches -- which is the whole reason the URL sits on the spec.
func TestExecuteVisitsThePageOnceAndChecksEveryCondition(t *testing.T) {
	spec := twoConditions(types.MatchAll)
	page := &selectorPage{text: map[string]string{".price": "$89.99", ".stock": "In stock"}}

	result, err := Execute(page, spec)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if page.navigations != 1 {
		t.Fatalf("navigated %d times, want exactly 1", page.navigations)
	}
	if len(page.waited) != 2 || page.waited[0] != ".price" || page.waited[1] != ".stock" {
		t.Fatalf("waited for %v, want each selector in spec order", page.waited)
	}
	if len(result.Checks) != 2 {
		t.Fatalf("checks = %+v, want one per condition", result.Checks)
	}
	if result.Checks[0].Value != 89.99 || result.Checks[1].Value != "In stock" {
		t.Fatalf("checks did not land in spec order: %+v", result.Checks)
	}
	if result.Checks[0].Key != spec.Conditions[0].Key() {
		t.Fatalf("check key = %q, want the condition's own", result.Checks[0].Key)
	}
	// The first condition is repeated at the top level, so {{value}} and every
	// result stored before this existed still read the same way.
	if result.Value != 89.99 || result.Raw != "$89.99" {
		t.Fatalf("the first condition must also be the top-level value: %+v", result)
	}
}
