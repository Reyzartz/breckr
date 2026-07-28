package types

import "time"

// Page is the slice of a browser page the executor needs.
//
// Narrow on purpose: it keeps the executor's operator table and extraction
// logic testable with a fake, and it is the seam that made swapping puppeteer
// for a Go CDP client a change in one package rather than everywhere.
//
// Implementations bind their own context, so a cancelled run aborts the
// in-flight CDP call rather than leaking it.
type Page interface {
	Navigate(url string) error
	// WaitForSelector blocks until the selector matches or the timeout expires.
	WaitForSelector(selector string, timeout time.Duration) error
	// Exists reports whether the selector matches, without waiting.
	Exists(selector string) (bool, error)
	// Count reports how many elements match, without waiting.
	Count(selector string) (int, error)
	Attribute(selector, name string) (string, error)
	Text(selector string) (string, error)
}

// ResolvedTask is an executable task, as the runner sees it.
//
// Tasks are authored from the dashboard as declarative TaskSpecs and turned
// into this shape by executor.Compile. Nothing constructs one by hand, but the
// runner is still written against this struct rather than against the spec --
// which is what keeps the run pipeline, the browser mutex and the edge-trigger
// state machine independent of how a task is described.
type ResolvedTask struct {
	// Stable identifier. Run history is keyed on it.
	ID   string
	Name string
	// Standard 5-field cron, evaluated in the configured timezone.
	Cron string
	// Covers connect and execution together.
	Timeout time.Duration
	// False for a task that needs no page at all -- the CDP connection is
	// skipped entirely, which is how the pipeline stays testable with no
	// browser. Every declarative spec reads a page, so in practice only tests
	// set this.
	NeedsBrowser bool
	// Extract what you want to watch.
	Run func(page Page) (*TaskResult, error)
	// True when the event you care about has happened. Edge-triggered.
	Condition func(result *TaskResult) (bool, error)
	// Message body for the alert.
	Notify func(result *TaskResult) string
}

// NotificationReason says why a notification did or did not go out.
//
// The caller must tell "error" from "disabled" apart: "error" still owes an
// alert and must leave the edge-trigger disarmed so the next run retries, while
// "disabled" owes nothing and advances state as if delivered.
type NotificationReason string

const (
	NotificationSent     NotificationReason = "sent"
	NotificationDisabled NotificationReason = "disabled"
	NotificationError    NotificationReason = "error"
)

type NotificationOutcome struct {
	Delivered bool
	Reason    NotificationReason
}
