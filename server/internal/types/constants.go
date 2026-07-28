package types

import (
	"regexp"
	"time"
)

var RunStatuses = []RunStatus{RunStatusRunning, RunStatusSuccess, RunStatusFailed}

func IsRunStatus(value string) bool {
	for _, status := range RunStatuses {
		if string(status) == value {
			return true
		}
	}
	return false
}

// --- API pagination ---------------------------------------------------------

const DefaultRunLimit = 50

// MaxRunLimit is the upper bound on `limit`, so one request cannot pull the
// whole history.
const MaxRunLimit = 200

// --- Scheduling -------------------------------------------------------------

// RetentionCron is the retention sweep: daily at 04:00 in the configured
// timezone.
const RetentionCron = "0 4 * * *"

// ScheduleFrequencies are the schedule shapes the dashboard's builder can send.
var ScheduleFrequencies = []string{
	FreqMinutes, FreqHours, FreqDay, FreqWeek, FreqMonth, FreqCustom,
}

// --- Telegram ---------------------------------------------------------------

const (
	TelegramAPIBase          = "https://api.telegram.org"
	TelegramTimeout          = 10 * time.Second
	TelegramMaxMessageLength = 4096
	TelegramTruncationSuffix = "\n… (truncated)"
)

// --- Browser ----------------------------------------------------------------

// BrowserProbeTimeout bounds the /api/health liveness probe, shorter than a
// real run.
const BrowserProbeTimeout = 5 * time.Second

// NavigationTimeout bounds the post-navigate DOMContentLoaded wait. Expiring is
// not an error -- see browser.rodPage.Navigate.
const NavigationTimeout = 15 * time.Second

// SelectorTimeout is how long to wait for a selector -- well under the default
// run timeout, so a selector that stopped matching fails as "waiting for
// .price" rather than as a generic run timeout that says nothing about which
// step stalled.
const SelectorTimeout = 10 * time.Second

// --- Task specs -------------------------------------------------------------

// TaskIDPattern keeps ids boring: they appear in URLs.
var TaskIDPattern = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

var ExtractKinds = []ExtractKind{
	ExtractText, ExtractNumber, ExtractAttribute, ExtractCount, ExtractExists,
}

// OperatorsByKind says which operators make sense for each kind.
//
// The pairing is enforced when a task is saved rather than when it runs: `gt`
// on an `exists` check would otherwise be a condition that can never fire, and
// a monitor that quietly never fires is the failure this app exists to avoid.
var OperatorsByKind = map[ExtractKind][]CompareOperator{
	ExtractText:      {OpEq, OpNeq, OpContains, OpNotContains, OpChanged},
	ExtractNumber:    {OpLT, OpLTE, OpGT, OpGTE, OpEq, OpNeq, OpChanged},
	ExtractAttribute: {OpEq, OpNeq, OpContains, OpNotContains, OpChanged},
	ExtractCount:     {OpLT, OpLTE, OpGT, OpGTE, OpEq, OpNeq, OpChanged},
	ExtractExists:    {OpIsTrue, OpIsFalse, OpChanged},
}

// ValuelessOperators test the value on its own, so spec.Value is not needed.
var ValuelessOperators = []CompareOperator{OpIsTrue, OpIsFalse, OpChanged}

// NumericKinds are the kinds whose spec.Value must parse as a finite number.
var NumericKinds = []ExtractKind{ExtractNumber, ExtractCount}

// MessagePlaceholders are the placeholders a message template may reference.
var MessagePlaceholders = []string{"value", "raw", "url", "name"}

// MessagePlaceholderPattern captures {{name}}, tolerating inner whitespace.
var MessagePlaceholderPattern = regexp.MustCompile(`\{\{\s*(\w+)\s*\}\}`)
