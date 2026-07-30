package executor

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"breckr-server/internal/types"
	"breckr-server/internal/utils"
)

/*
Validation for user-authored task specs.

Specs arrive over HTTP from the dashboard, so this is the only thing standing
between a typo and a monitor that silently never fires. Every rejection has to
say what to fix -- the message is shown against the offending field in the form.

Pure: no database, no browser, no config. That is what makes the whole rejection
table testable without either.
*/

func requireString(value string, field, label string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", utils.Fail(field, "%s must be a non-empty string.", label)
	}
	return trimmed, nil
}

// validateURL checks the scheme, because a spec's URL is handed straight to a
// real browser: `file:` would read the container's filesystem and `javascript:`
// would execute in the page -- neither is something a monitor should reach.
func validateURL(raw string) (string, error) {
	value, err := requireString(raw, "url", "`url`")
	if err != nil {
		return "", err
	}

	parsed, err := url.Parse(value)
	if err != nil || !parsed.IsAbs() {
		return "", utils.Fail("url", "`url` %q is not a valid absolute URL.", value)
	}

	// Checked before the host, so "file:///etc/passwd" and "javascript:alert(1)"
	// -- which have no host either -- are rejected for the reason that actually
	// matters rather than as generic malformed URLs.
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", utils.Fail("url", "`url` must be http:// or https://, got \"%s:\".", parsed.Scheme)
	}

	if parsed.Host == "" {
		return "", utils.Fail("url", "`url` %q is not a valid absolute URL.", value)
	}

	return parsed.String(), nil
}

// conditionField names a field inside one condition.
//
// The dashboard renders a server complaint against the control it names, and
// with a list of conditions a bare "selector" would land on whichever row
// happens to be first -- which is the one row you can be sure is not the
// problem, since validation stops at the first failure.
func conditionField(index int, name string) string {
	return fmt.Sprintf("conditions[%d].%s", index, name)
}

func validateExtract(raw types.ExtractKind, index int) (types.ExtractKind, error) {
	for _, kind := range types.ExtractKinds {
		if kind == raw {
			return raw, nil
		}
	}

	labels := make([]string, len(types.ExtractKinds))
	for i, kind := range types.ExtractKinds {
		labels[i] = string(kind)
	}
	return "", utils.Fail(conditionField(index, "extract"),
		"`extract` must be one of %s, got %q.", strings.Join(labels, ", "), string(raw))
}

func validateOperator(raw types.CompareOperator, extract types.ExtractKind, index int) (types.CompareOperator, error) {
	allowed := types.OperatorsByKind[extract]

	for _, operator := range allowed {
		if operator == raw {
			return raw, nil
		}
	}

	labels := make([]string, len(allowed))
	for i, operator := range allowed {
		labels[i] = string(operator)
	}
	return "", utils.Fail(conditionField(index, "operator"),
		"`operator` %q cannot be used with extract %q. Allowed: %s.",
		string(raw), string(extract), strings.Join(labels, ", "))
}

func validateMatch(raw types.MatchMode) (types.MatchMode, error) {
	if strings.TrimSpace(string(raw)) == "" {
		return types.DefaultMatchMode, nil
	}
	if !types.IsMatchMode(string(raw)) {
		labels := make([]string, len(types.MatchModes))
		for i, mode := range types.MatchModes {
			labels[i] = string(mode)
		}
		return "", utils.Fail("match",
			"`match` must be one of %s, got %q.", strings.Join(labels, ", "), string(raw))
	}
	return raw, nil
}

// validateMessage rejects an unknown placeholder rather than rendering it
// literally: `{{prive}}` would otherwise ship in the alert body as-is, and you
// would only find out at the moment you most wanted the message to be right.
//
// `conditions` is how many the task has, which is what makes {{value3}} on a
// two-condition task answerable here rather than at alert time.
func validateMessage(raw string, conditions int) (string, error) {
	message := strings.TrimSpace(raw)
	if message == "" {
		return "", nil
	}

	for _, match := range types.MessagePlaceholderPattern.FindAllStringSubmatch(message, -1) {
		name := match[1]

		if indexed := types.IndexedPlaceholderPattern.FindStringSubmatch(name); indexed != nil {
			// The pattern only matches digits without a leading zero, so this
			// parses unless the number is longer than an int -- which is out of
			// range for any condition count regardless.
			position, err := strconv.Atoi(indexed[2])
			if err != nil || position > conditions {
				return "", utils.Fail("message",
					"`message` references {{%s}}, but the task has %s.",
					name, pluralConditions(conditions))
			}
			continue
		}

		known := false
		for _, placeholder := range types.MessagePlaceholders {
			if placeholder == name {
				known = true
				break
			}
		}
		if !known {
			available := make([]string, len(types.MessagePlaceholders))
			for i, placeholder := range types.MessagePlaceholders {
				available[i] = "{{" + placeholder + "}}"
			}
			return "", utils.Fail("message",
				"`message` references unknown placeholder {{%s}}. Available: %s, "+
					"plus {{value1}} / {{raw1}} … one pair per condition.",
				name, strings.Join(available, ", "))
		}
	}

	return message, nil
}

func pluralConditions(count int) string {
	if count == 1 {
		return "only 1 condition"
	}
	return fmt.Sprintf("only %d conditions", count)
}

func isValueless(operator types.CompareOperator) bool {
	for _, candidate := range types.ValuelessOperators {
		if candidate == operator {
			return true
		}
	}
	return false
}

func isNumericKind(extract types.ExtractKind) bool {
	for _, candidate := range types.NumericKinds {
		if candidate == extract {
			return true
		}
	}
	return false
}

// validateCondition validates one condition and returns it normalized, with
// blanks collapsed away.
func validateCondition(candidate *types.Condition, index int) (types.Condition, error) {
	selector, err := requireString(candidate.Selector, conditionField(index, "selector"), "`selector`")
	if err != nil {
		return types.Condition{}, err
	}

	waitForSelector := strings.TrimSpace(candidate.WaitForSelector)

	extract, err := validateExtract(candidate.Extract, index)
	if err != nil {
		return types.Condition{}, err
	}

	operator, err := validateOperator(candidate.Operator, extract, index)
	if err != nil {
		return types.Condition{}, err
	}

	// Only meaningful for `attribute`, and dropped otherwise so a leftover
	// value from switching kinds in the form does not linger in the stored spec.
	attribute := ""
	if extract == types.ExtractAttribute {
		attribute, err = requireString(candidate.Attribute, conditionField(index, "attribute"),
			"`attribute` is required when extract is \"attribute\" and")
		if err != nil {
			return types.Condition{}, err
		}
	}

	value := ""
	if !isValueless(operator) {
		value, err = requireString(candidate.Value, conditionField(index, "value"),
			fmt.Sprintf("`value` is required for operator %q and", string(operator)))
		if err != nil {
			return types.Condition{}, err
		}

		if isNumericKind(extract) {
			if _, err := strconv.ParseFloat(value, 64); err != nil {
				return types.Condition{}, utils.Fail(conditionField(index, "value"),
					"`value` must be a number when extract is %q, got %q.", string(extract), value)
			}
		}
	}

	return types.Condition{
		Selector:        selector,
		WaitForSelector: waitForSelector,
		Extract:         extract,
		Operator:        operator,
		Attribute:       attribute,
		Value:           value,
	}, nil
}

// ValidateSpec validates a spec and returns it normalized, with blanks
// collapsed away.
//
// A spec sent in the single-condition shape that came before Conditions was a
// list has already been hoisted into one by TaskSpec.UnmarshalJSON, so there is
// only one shape to validate here.
func ValidateSpec(candidate *types.TaskSpec) (*types.TaskSpec, error) {
	if candidate == nil {
		return nil, utils.Fail("spec", "`spec` must be an object.")
	}

	specURL, err := validateURL(candidate.URL)
	if err != nil {
		return nil, err
	}

	match, err := validateMatch(candidate.Match)
	if err != nil {
		return nil, err
	}

	switch {
	case len(candidate.Conditions) == 0:
		return nil, utils.Fail("conditions", "A task needs at least one condition.")
	case len(candidate.Conditions) > types.MaxConditions:
		return nil, utils.Fail("conditions",
			"A task can have at most %d conditions, got %d.",
			types.MaxConditions, len(candidate.Conditions))
	}

	conditions := make([]types.Condition, len(candidate.Conditions))
	for i := range candidate.Conditions {
		conditions[i], err = validateCondition(&candidate.Conditions[i], i)
		if err != nil {
			return nil, err
		}
	}

	// Last, so it can be checked against the condition count the spec actually
	// ended up with.
	message, err := validateMessage(candidate.Message, len(conditions))
	if err != nil {
		return nil, err
	}

	return &types.TaskSpec{
		URL:        specURL,
		Match:      match,
		Conditions: conditions,
		Message:    message,
	}, nil
}

// ValidateNotifyMode resolves the alert mode, defaulting an empty value.
//
// Absent means "whatever the default is" rather than an error: the mode is an
// optional field on both create and patch, and a caller driving the API by hand
// should not have to name it to get the behavior every task had before it
// existed.
func ValidateNotifyMode(raw types.NotifyMode) (types.NotifyMode, error) {
	if strings.TrimSpace(string(raw)) == "" {
		return types.DefaultNotifyMode, nil
	}
	if !types.IsNotifyMode(string(raw)) {
		labels := make([]string, len(types.NotifyModes))
		for i, mode := range types.NotifyModes {
			labels[i] = string(mode)
		}
		return "", utils.Fail("notify_mode",
			"`notify_mode` must be one of %s, got %q.", strings.Join(labels, ", "), string(raw))
	}
	return raw, nil
}

func ValidateTaskID(raw string) (string, error) {
	id, err := requireString(raw, "id", "`id`")
	if err != nil {
		return "", err
	}
	if !types.TaskIDPattern.MatchString(id) {
		return "", utils.Fail("id", "`id` %q must contain only letters, digits, . _ or -.", id)
	}
	return id, nil
}

func ValidateName(raw string) (string, error) {
	return requireString(raw, "name", "`name`")
}

func ValidateCron(raw string) (string, error) {
	expr, err := requireString(raw, "cron_expr", "`cron_expr`")
	if err != nil {
		return "", err
	}
	if !ValidateCronExpr(expr) {
		return "", utils.Fail("cron_expr", "`cron_expr` %q is not a valid cron expression.", expr)
	}
	return expr, nil
}

// ResolveCron resolves the cron a request means, from whichever field it used.
//
// The dashboard sends a structured `schedule`; a caller driving the API by hand
// can still send `cron_expr`. `schedule` wins so a client that sends both is not
// silently scheduled on the stale one.
func ResolveCron(schedule *types.Schedule, cronExpr *string) (string, error) {
	if schedule != nil {
		return ToCron(schedule)
	}
	if cronExpr != nil {
		return ValidateCron(*cronExpr)
	}
	return "", utils.Fail("schedule", "A `schedule` or a `cron_expr` is required.")
}
