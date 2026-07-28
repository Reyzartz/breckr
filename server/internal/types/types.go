// Package types is the HTTP contract, mirrored by client/src/types/index.ts.
//
// The json tags are load-bearing and deliberately inconsistent: the contract
// mixes snake_case for stored columns (cron_expr, condition_met) with camelCase
// for computed values (waitForSelector, checkedAt, conditionMet). The dashboard
// depends on both spellings exactly as they are.
package types

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
	// Joined from tasks; null if the task row has since been removed.
	TaskName *string `json:"task_name"`
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

// TaskSpec is a task's behavior, declared rather than coded.
//
// Interpreted at run time by the executor, so nothing here is ever evaluated as
// code -- which is what makes it safe to author from the dashboard.
type TaskSpec struct {
	// http/https only; it is handed straight to a real browser.
	URL string `json:"url"`
	// Waited for before extraction. Defaults to Selector when omitted.
	WaitForSelector string      `json:"waitForSelector,omitempty"`
	Selector        string      `json:"selector"`
	Extract         ExtractKind `json:"extract"`
	// Required when Extract is "attribute", ignored otherwise.
	Attribute string          `json:"attribute,omitempty"`
	Operator  CompareOperator `json:"operator"`
	// Required except for "is_true", "is_false" and "changed".
	Value string `json:"value,omitempty"`
	// Alert body. Supports {{value}}, {{raw}}, {{url}} and {{name}}.
	Message string `json:"message,omitempty"`
}

// TaskResult is what a spec-driven run returns, and what is stored as the run's
// result.
type TaskResult struct {
	// The typed extraction: number, string, or boolean depending on the kind.
	Value any `json:"value"`
	// The untouched text the value was derived from.
	Raw string `json:"raw"`
	URL string `json:"url"`
	// ISO-8601.
	CheckedAt string `json:"checkedAt"`
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
	ConditionMet bool `json:"condition_met"`
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

type HealthResponse struct {
	OK       bool          `json:"ok"`
	Browser  BrowserHealth `json:"browser"`
	Tasks    int           `json:"tasks"`
	Timezone string        `json:"timezone"`
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
	// Defaults to true.
	Enabled *bool `json:"enabled,omitempty"`
}

// UpdateTaskRequest patches a task; only what is present is changed.
type UpdateTaskRequest struct {
	Enabled *bool   `json:"enabled,omitempty"`
	Name    *string `json:"name,omitempty"`
	// Takes precedence over CronExpr when both are sent.
	Schedule *Schedule `json:"schedule,omitempty"`
	CronExpr *string   `json:"cron_expr,omitempty"`
	Spec     *TaskSpec `json:"spec,omitempty"`
}

// TestTaskRequest is a draft task, run once without being saved.
type TestTaskRequest struct {
	// Only used to render {{name}} in the message template.
	Name string    `json:"name,omitempty"`
	Spec *TaskSpec `json:"spec"`
}
