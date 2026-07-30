// Package types is the HTTP contract, mirrored by client/src/types/index.ts.
//
// The json tags are load-bearing and deliberately inconsistent: the contract
// mixes snake_case for stored columns (cron_expr, condition_met) with camelCase
// for computed values (waitForSelector, checkedAt, conditionMet). The dashboard
// depends on both spellings exactly as they are.
package types

import (
	"encoding/json"
	"fmt"
)

// RunStatus is the terminal state of a run. "running" is written before
// execution starts.
type RunStatus string

const (
	RunStatusRunning RunStatus = "running"
	RunStatusSuccess RunStatus = "success"
	RunStatusFailed  RunStatus = "failed"
)

// TriggerSource is how a run was started.
type TriggerSource string

const (
	TriggerCron   TriggerSource = "cron"
	TriggerManual TriggerSource = "manual"
)

// Run is one execution of a task.
type Run struct {
	ID     int64  `json:"id"`
	TaskID string `json:"task_id"`
	// ISO-8601. Written before execution, so it always exists.
	StartedAt string `json:"started_at"`
	// ISO-8601, or null while the run is still in flight.
	FinishedAt *string   `json:"finished_at"`
	Status     RunStatus `json:"status"`
	// Whether the task's condition matched on this run.
	ConditionMet bool `json:"condition_met"`
	// Whether an alert was actually delivered for this run.
	Notified      bool          `json:"notified"`
	TriggerSource TriggerSource `json:"trigger_source"`
	// JSON-encoded return value of the run, or null when it failed.
	ResultSummary *string `json:"result_summary"`
	// Message and stack when the run failed.
	Error *string `json:"error"`
	// Why an alert did or did not go out, or null when none was owed because the
	// condition did not transition. Notified says whether one arrived; this says
	// why it did not, which a bool cannot.
	NotificationStatus *NotificationReason `json:"notification_status"`
	// The failure reason when NotificationStatus is "error" or "disabled".
	NotificationDetail *string `json:"notification_detail"`
	// The alert body handed to the notifier, so what was sent is inspectable.
	NotificationMessage *string `json:"notification_message"`
	// Joined from tasks; null if the task row has since been removed.
	TaskName *string `json:"task_name"`
	// Per-channel breakdown behind the aggregate above. Populated only by
	// GET /api/runs/{id}: the list view shows dozens of runs and does not need
	// a second query per row to render a status badge.
	Attempts []*NotificationAttempt `json:"attempts,omitempty"`
}

// --- Task specs -------------------------------------------------------------

// ExtractKind is what to pull out of the page once the selector has matched.
type ExtractKind string

const (
	ExtractText      ExtractKind = "text"
	ExtractNumber    ExtractKind = "number"
	ExtractAttribute ExtractKind = "attribute"
	ExtractCount     ExtractKind = "count"
	ExtractExists    ExtractKind = "exists"
)

// CompareOperator is how the extracted value is tested.
//
// Not every operator applies to every ExtractKind -- the server rejects an
// invalid pairing when the task is saved, rather than letting it surface as a
// condition that can never fire.
type CompareOperator string

const (
	OpLT          CompareOperator = "lt"
	OpLTE         CompareOperator = "lte"
	OpGT          CompareOperator = "gt"
	OpGTE         CompareOperator = "gte"
	OpEq          CompareOperator = "eq"
	OpNeq         CompareOperator = "neq"
	OpContains    CompareOperator = "contains"
	OpNotContains CompareOperator = "not_contains"
	OpIsTrue      CompareOperator = "is_true"
	OpIsFalse     CompareOperator = "is_false"
	OpChanged     CompareOperator = "changed"
)

// MatchMode says how a task's conditions combine into the single true/false
// that drives the alert.
//
// One mode for the whole task rather than a boolean tree: "all of these" and
// "any of these" cover what a monitor actually needs, and a flat list is a
// thing you can read off the dashboard at a glance -- which matters more here
// than expressiveness, because the failure this app exists to avoid is a
// condition nobody realized could never fire.
type MatchMode string

const (
	// MatchAll alerts when every condition is met.
	MatchAll MatchMode = "all"
	// MatchAny alerts when at least one condition is met.
	MatchAny MatchMode = "any"
)

var MatchModes = []MatchMode{MatchAll, MatchAny}

// DefaultMatchMode is what a spec means when it says nothing -- which is every
// spec stored before conditions became a list, each of which had exactly one.
const DefaultMatchMode = MatchAll

func IsMatchMode(value string) bool {
	for _, mode := range MatchModes {
		if string(mode) == value {
			return true
		}
	}
	return false
}

// Condition is one thing to watch on the page: what to select, what to pull out
// of it, and what would make that interesting.
//
// Every condition in a task reads the same page -- the URL lives on the spec.
// Watching two sites is two tasks, which keeps one run to one navigation and
// keeps a failure attributable to a single page.
type Condition struct {
	Selector string `json:"selector"`
	// Waited for before extraction. Defaults to Selector when omitted.
	WaitForSelector string      `json:"waitForSelector,omitempty"`
	Extract         ExtractKind `json:"extract"`
	// Required when Extract is "attribute", ignored otherwise.
	Attribute string          `json:"attribute,omitempty"`
	Operator  CompareOperator `json:"operator"`
	// Required except for "is_true", "is_false" and "changed".
	Value string `json:"value,omitempty"`
}

// Key identifies a condition by what it extracts rather than by where it sits
// in the list.
//
// The `changed` operator compares against the last successful run, so it needs
// to find *its own* previous value in a result recorded under an older version
// of the spec. Keying on position would silently compare against a sibling the
// moment a condition is reordered or one is inserted above it; keying on the
// extraction means a genuinely edited condition simply finds nothing, which
// reads as "no change" and costs at most one skipped alert.
//
// Quoted rather than joined raw, so a selector containing the separator cannot
// collide with a different condition.
func (c Condition) Key() string {
	return fmt.Sprintf("%q|%s|%q", c.Selector, c.Extract, c.Attribute)
}

// TaskSpec is a task's behavior, declared rather than coded.
//
// Interpreted at run time by the executor, so nothing here is ever evaluated as
// code -- which is what makes it safe to author from the dashboard.
type TaskSpec struct {
	// http/https only; it is handed straight to a real browser.
	URL string `json:"url"`
	// How Conditions combine. Empty means MatchAll.
	Match MatchMode `json:"match,omitempty"`
	// At least one, at most MaxConditions. Order is the order they are checked
	// and the order {{value1}}, {{value2}} … refer to.
	Conditions []Condition `json:"conditions"`
	// Alert body. Supports {{value}}, {{raw}}, {{url}}, {{name}} and the indexed
	// {{value1}} / {{raw1}} … one pair per condition.
	Message string `json:"message,omitempty"`
}

// UnmarshalJSON accepts the single-condition shape that came before Conditions
// was a list, and hoists it into the list.
//
// This is the only migration there is, and it is deliberately not a SQL one:
// every spec is an opaque JSON blob in one column, so rewriting them in place
// would mean a migration that parses and re-encodes user data with no way to
// undo it. Doing it on decode instead makes both readers total -- the stored
// row and the request from a client that has not been updated take the same
// path -- and a hoisted spec is written back in the new shape the next time it
// is saved.
func (s *TaskSpec) UnmarshalJSON(data []byte) error {
	// The union of both shapes. Not an alias of TaskSpec, because the fields
	// being hoisted are no longer on it.
	var wire struct {
		URL        string      `json:"url"`
		Match      MatchMode   `json:"match"`
		Conditions []Condition `json:"conditions"`
		Message    string      `json:"message"`

		// The pre-list shape.
		Selector        string          `json:"selector"`
		WaitForSelector string          `json:"waitForSelector"`
		Extract         ExtractKind     `json:"extract"`
		Attribute       string          `json:"attribute"`
		Operator        CompareOperator `json:"operator"`
		Value           string          `json:"value"`
	}

	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}

	conditions := wire.Conditions

	// Hoisted on any sign of the old shape rather than only on a usable one, so
	// a legacy request with a blank selector still lands as one condition and is
	// rejected with a message naming that condition's field -- not as "a task
	// needs at least one condition", which would say nothing about what to fix.
	if len(conditions) == 0 &&
		(wire.Selector != "" || wire.Extract != "" || wire.Operator != "") {
		conditions = []Condition{{
			Selector:        wire.Selector,
			WaitForSelector: wire.WaitForSelector,
			Extract:         wire.Extract,
			Attribute:       wire.Attribute,
			Operator:        wire.Operator,
			Value:           wire.Value,
		}}
	}

	match := wire.Match
	if match == "" {
		match = DefaultMatchMode
	}

	*s = TaskSpec{
		URL:        wire.URL,
		Match:      match,
		Conditions: conditions,
		Message:    wire.Message,
	}
	return nil
}

// CheckResult is what one condition saw on one run.
type CheckResult struct {
	// Identifies the condition that produced it -- see Condition.Key.
	Key string `json:"key"`
	// The typed extraction: number, string, or boolean depending on the kind.
	Value any `json:"value"`
	// The untouched text the value was derived from.
	Raw string `json:"raw"`
	// Whether this condition's operator matched. Written by the condition step
	// rather than the extraction step, so it is only meaningful once the run has
	// been evaluated.
	Met bool `json:"met"`
}

// TaskResult is what a spec-driven run returns, and what is stored as the run's
// result.
type TaskResult struct {
	// The first condition's extraction, repeated here so {{value}} keeps meaning
	// what it always meant and so a result stored before conditions became a
	// list still reads back the same way.
	Value any `json:"value"`
	// The first condition's untouched text, for the same reason.
	Raw string `json:"raw"`
	URL string `json:"url"`
	// ISO-8601.
	CheckedAt string `json:"checkedAt"`
	// One entry per condition, in spec order. Absent on a result stored before
	// conditions became a list.
	Checks []CheckResult `json:"checks,omitempty"`
}

// --- Schedules --------------------------------------------------------------

// Schedule is a schedule as the dashboard builds it, before it becomes cron.
//
// Cron stays the storage format: the server converts a Schedule to cron_expr on
// the way in and derives one back from the stored expression on the way out, so
// nothing but the server ever handles a cron string.
//
// "custom" exists because that derivation has to be total. An expression the
// builder cannot express -- a hand-written row, a range, the six-field form --
// comes back as custom and survives an edit untouched, rather than being
// silently rewritten into the nearest shape the builder does have a control for.
//
// The TypeScript side is a discriminated union on `every`. Pointer fields plus
// omitempty marshal to exactly those shapes: only the keys that belong to the
// active variant are present.
type Schedule struct {
	Every string `json:"every"`
	// minutes, hours
	Interval *int `json:"interval,omitempty"`
	// hours, day, week, month
	Minute *int `json:"minute,omitempty"`
	// day, week, month
	Hour *int `json:"hour,omitempty"`
	// month
	Day *int `json:"day,omitempty"`
	// week; cron's 0-6 with 0 = Sunday.
	Weekdays []int `json:"weekdays,omitempty"`
	// custom
	Cron *string `json:"cron,omitempty"`
}

const (
	FreqMinutes = "minutes"
	FreqHours   = "hours"
	FreqDay     = "day"
	FreqWeek    = "week"
	FreqMonth   = "month"
	FreqCustom  = "custom"
)

// --- Tasks ------------------------------------------------------------------

// NotifyMode is when a task alerts, given a condition that is met.
//
// Edge-triggering is the default because a condition that stays true is the
// normal case for a monitor -- a price that dropped stays dropped -- and
// alerting on every interval would train you to ignore the alerts. "always"
// exists for the tasks where each matching run is its own event.
type NotifyMode string

const (
	// NotifyOnTransition alerts on the false -> true transition only, and not
	// again until the condition goes back to false.
	NotifyOnTransition NotifyMode = "transition"
	// NotifyAlways alerts on every run whose condition is met.
	NotifyAlways NotifyMode = "always"
)

var NotifyModes = []NotifyMode{NotifyOnTransition, NotifyAlways}

// DefaultNotifyMode is what a task alerts on when it says nothing -- and what
// the column defaults to, so a row written before the mode existed keeps
// behaving exactly as it did.
const DefaultNotifyMode = NotifyOnTransition

func IsNotifyMode(value string) bool {
	for _, mode := range NotifyModes {
		if string(mode) == value {
			return true
		}
	}
	return false
}

// Task is a task as stored.
type Task struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	CronExpr string `json:"cron_expr"`
	Enabled  bool   `json:"enabled"`
	// Null only for a legacy row written before tasks moved into the database,
	// or one whose stored JSON no longer parses. Such a task keeps its history
	// but cannot be scheduled -- see TaskWithStatus.Orphaned.
	Spec *TaskSpec `json:"spec"`
	// Last known result of the condition. Drives edge-triggering: an alert
	// fires only on the false -> true transition, and this persists across
	// restarts so a reboot cannot re-notify.
	//
	// Tracked even under NotifyAlways, which ignores it: switching a task back
	// to "transition" has to land on the real state of the condition rather
	// than on whatever it was when the mode was changed.
	ConditionMet bool `json:"condition_met"`
	// When to alert while the condition is met. Defaults to "transition".
	NotifyMode NotifyMode `json:"notify_mode"`
	// ISO-8601 of the last delivered alert, or null if none has been sent.
	LastNotifiedAt *string `json:"last_notified_at"`
}

// TaskWithStatus is a task decorated with scheduling and history for the
// dashboard.
type TaskWithStatus struct {
	Task
	// CronExpr in the shape the form's builder edits. Derived on read and never
	// stored, so a row whose expression was written by hand still opens in the
	// form -- as custom, carrying the expression through unchanged.
	Schedule Schedule `json:"schedule"`
	LastRun  *Run     `json:"last_run"`
	// ISO-8601 of the next scheduled fire, or null while disabled.
	NextRun *string `json:"next_run"`
	// True when the row carries no usable spec -- it keeps its history but can
	// no longer be run, and the dashboard offers only deletion.
	Orphaned bool `json:"orphaned"`
	// Channels this task alerts to, as saved. Includes disabled ones: the form
	// shows the links the user made, not the ones that would deliver right now.
	ChannelIDs []string `json:"channel_ids"`
}

// --- Channels ---------------------------------------------------------------

// ChannelType selects which transport delivers an alert. The stored config blob
// is parsed according to it, so it is the discriminator for everything else on
// the row.
type ChannelType string

const (
	ChannelTelegram ChannelType = "telegram"
	ChannelDiscord  ChannelType = "discord"
	ChannelSlack    ChannelType = "slack"
	ChannelWebhook  ChannelType = "webhook"
	ChannelEmail    ChannelType = "email"
)

var ChannelTypes = []ChannelType{
	ChannelTelegram, ChannelDiscord, ChannelSlack, ChannelWebhook, ChannelEmail,
}

func IsChannelType(value string) bool {
	for _, kind := range ChannelTypes {
		if string(kind) == value {
			return true
		}
	}
	return false
}

// Channel is a delivery destination as the API returns it.
//
// Config carries the *redacted* view -- secrets are masked to their last four
// characters. The decrypted config never leaves the notifier package, so no
// response, log line or error message can carry a token by accident.
type Channel struct {
	ID      string         `json:"id"`
	Name    string         `json:"name"`
	Type    ChannelType    `json:"type"`
	Enabled bool           `json:"enabled"`
	Config  map[string]any `json:"config"`
	// True when the stored config could not be decrypted or no longer parses --
	// almost always a replaced key file. The channel keeps its identity so the
	// dashboard can say which one to re-enter, rather than the row vanishing.
	Broken    bool   `json:"broken"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// NotificationAttempt is one channel's outcome for one run.
//
// ChannelID goes null when the channel is deleted, but the name and type are
// copies -- history stays readable after the destination is gone.
type NotificationAttempt struct {
	ID          int64              `json:"id"`
	RunID       int64              `json:"run_id"`
	ChannelID   *string            `json:"channel_id"`
	ChannelName string             `json:"channel_name"`
	ChannelType ChannelType        `json:"channel_type"`
	Status      NotificationReason `json:"status"`
	Detail      *string            `json:"detail"`
	Message     *string            `json:"message"`
	AttemptedAt string             `json:"attempted_at"`
}

// --- Responses --------------------------------------------------------------

type RunsResponse struct {
	Total  int    `json:"total"`
	Runs   []*Run `json:"runs"`
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
}

type BrowserHealth struct {
	Endpoint  string `json:"endpoint"`
	Reachable bool   `json:"reachable"`
	// Present when reachable.
	Version string `json:"version,omitempty"`
	// Present when not reachable.
	Error string `json:"error,omitempty"`
}

// NotifierHealth reports whether alerts can be delivered at all, so the
// dashboard can warn about a silent monitor before anyone waits on an alert
// that was never going to arrive.
//
// Configured now means "at least one enabled channel exists" rather than "the
// env vars are set" -- with channels being rows, an empty table is the new way
// to be silently unreachable.
type NotifierHealth struct {
	Configured bool `json:"configured"`
	Channels   int  `json:"channels"`
}

type HealthResponse struct {
	OK      bool          `json:"ok"`
	Browser BrowserHealth `json:"browser"`
	// Notifications being unconfigured is reported, not fatal: runs still
	// happen and still record their outcome, they just cannot alert.
	Notifications NotifierHealth `json:"notifications"`
	Tasks         int            `json:"tasks"`
	Timezone      string         `json:"timezone"`
}

// TestNotificationResponse is the outcome of a channel test: one real delivery
// attempt, on demand.
//
// Always returned with 200. A rejection by the transport is a successful report
// of a failed delivery, not an HTTP error -- same as TestTaskResponse.
type TestNotificationResponse struct {
	OK     bool               `json:"ok"`
	Status NotificationReason `json:"status"`
	// Why it did not arrive. Present when not delivered.
	Detail string `json:"detail,omitempty"`
	// Echoed so the dashboard can show exactly what was sent.
	Message string `json:"message"`
	// ISO-8601 of the attempt.
	AttemptedAt string `json:"attemptedAt"`
}

type UpdateTaskResponse struct {
	ID      string  `json:"id"`
	Enabled bool    `json:"enabled"`
	NextRun *string `json:"next_run"`
}

// TestTaskResponse is the outcome of POST /api/tasks/test: one execution of a
// draft spec.
//
// Writes no run row and sends no notification, so it can be pressed freely
// while getting a selector right.
type TestTaskResponse struct {
	OK bool `json:"ok"`
	// Present when OK.
	Result *TaskResult `json:"result,omitempty"`
	// Whether the draft's condition matched this extraction.
	ConditionMet *bool `json:"conditionMet,omitempty"`
	// The alert body that would have been sent, rendered from the template.
	Message string `json:"message,omitempty"`
	// Present when the run failed -- a bad selector, a timeout, a dead URL.
	Error string `json:"error,omitempty"`
}

// RunOutcome is the outcome of a single run, returned by
// POST /api/tasks/:id/run-now.
type RunOutcome struct {
	RunID        int64     `json:"runId"`
	Status       RunStatus `json:"status"`
	ConditionMet bool      `json:"conditionMet"`
	Notified     bool      `json:"notified"`
	Error        string    `json:"error,omitempty"`
}

// --- Requests ---------------------------------------------------------------

// CreateTaskRequest and UpdateTaskRequest are decoded into map[string]any
// rather than a struct: the PATCH route has to distinguish "field absent" from
// "field present and empty", and every value is re-validated by the spec
// package anyway. These types document the shape the dashboard sends.
type CreateTaskRequest struct {
	// Stable identifier. Run history is keyed on it, so it cannot be changed.
	ID   string `json:"id"`
	Name string `json:"name"`
	// The schedule to run on. Exactly one of Schedule and CronExpr is required;
	// Schedule wins when both are sent. The dashboard sends this one.
	Schedule *Schedule `json:"schedule,omitempty"`
	// Standard 5-field cron, evaluated in the server's configured timezone.
	// Kept for callers driving the API directly.
	CronExpr string    `json:"cron_expr,omitempty"`
	Spec     *TaskSpec `json:"spec"`
	// When to alert while the condition is met. Defaults to "transition".
	NotifyMode *NotifyMode `json:"notify_mode,omitempty"`
	// Defaults to true.
	Enabled *bool `json:"enabled,omitempty"`
	// Channels to alert on. Empty is allowed -- a task that only records history
	// is legitimate -- but the dashboard warns, since a monitor that cannot
	// alert is usually a mistake rather than a choice.
	ChannelIDs []string `json:"channel_ids,omitempty"`
}

// UpdateTaskRequest patches a task; only what is present is changed.
type UpdateTaskRequest struct {
	Enabled *bool   `json:"enabled,omitempty"`
	Name    *string `json:"name,omitempty"`
	// Takes precedence over CronExpr when both are sent.
	Schedule *Schedule `json:"schedule,omitempty"`
	CronExpr *string   `json:"cron_expr,omitempty"`
	Spec     *TaskSpec `json:"spec,omitempty"`
	// Absent leaves the mode alone. Changing it deliberately does *not* re-arm
	// the edge-trigger -- see SQLiteTaskStore.UpdateTask.
	NotifyMode *NotifyMode `json:"notify_mode,omitempty"`
	// Absent leaves the links alone; present replaces them wholesale, including
	// with [] to detach every channel.
	ChannelIDs *[]string `json:"channel_ids,omitempty"`
}

// CreateChannelRequest creates a delivery destination. Config is left raw here
// and parsed according to Type, which is the only thing that knows its shape.
type CreateChannelRequest struct {
	Name string          `json:"name"`
	Type ChannelType     `json:"type"`
	Config json.RawMessage `json:"config"`
	// Defaults to true.
	Enabled *bool `json:"enabled,omitempty"`
}

// UpdateChannelRequest patches a channel; only what is present is changed.
//
// An absent Config keeps the stored credentials, so renaming or muting a channel
// does not mean re-typing a token the dashboard was never shown.
type UpdateChannelRequest struct {
	Name    *string         `json:"name,omitempty"`
	Config  json.RawMessage `json:"config,omitempty"`
	Enabled *bool           `json:"enabled,omitempty"`
}

// TestChannelRequest tests a channel that has not been saved yet, so a
// misconfiguration is caught while the form is still open.
type TestChannelRequest struct {
	Type   ChannelType     `json:"type"`
	Config json.RawMessage `json:"config"`
}

// TestTaskRequest is a draft task, run once without being saved.
type TestTaskRequest struct {
	// Only used to render {{name}} in the message template.
	Name string    `json:"name,omitempty"`
	Spec *TaskSpec `json:"spec"`
}
