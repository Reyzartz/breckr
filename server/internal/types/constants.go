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

// --- Events -----------------------------------------------------------------

// EventsPingInterval is how often /api/events pings an otherwise idle socket. A
// peer that vanished without a close frame -- a closed laptop, a dropped
// network -- is only detectable by asking.
const EventsPingInterval = 30 * time.Second

// EventsWriteTimeout bounds a single frame, event or ping. Generous for what is
// normally a loopback socket, short enough that a wedged peer is dropped rather
// than held open forever.
const EventsWriteTimeout = 10 * time.Second

// HealthProbeInterval is how often the server checks the browser on its own
// behalf, publishing only when the answer changes.
//
// Reachability lives in another process, so it is the one piece of dashboard
// state nobody can push -- somebody has to ask. Asking here, once, replaces
// every open dashboard asking on its own timer.
const HealthProbeInterval = 30 * time.Second

// ScheduleFrequencies are the schedule shapes the dashboard's builder can send.
var ScheduleFrequencies = []string{
	FreqMinutes, FreqHours, FreqDay, FreqWeek, FreqMonth, FreqCustom,
}

// --- Notification transports ------------------------------------------------

// NotifyTimeout bounds one delivery attempt, whatever the transport. Channels
// fan out in parallel, so it is the ceiling on the whole notification step
// rather than a per-channel cost the run pays serially.
const NotifyTimeout = 10 * time.Second

// TruncationSuffix marks a message that hit its destination's length cap.
const TruncationSuffix = "\n… (truncated)"

// ErrorBodyLimit caps how much of a rejection body is kept as the failure
// detail. Enough for the reason, not enough for an HTML error page to fill the
// run row.
const ErrorBodyLimit = 500

const (
	TelegramAPIBase          = "https://api.telegram.org"
	TelegramMaxMessageLength = 4096
	DiscordMaxMessageLength  = 2000
	SlackMaxMessageLength    = 3000
)

const (
	DefaultSMTPHost = "smtp.gmail.com"
	// 587 is STARTTLS. 465 (implicit TLS) is not supported: one handshake path
	// is enough, and Gmail serves both.
	DefaultSMTPPort = 587
	// DefaultEmailSubject is used when an alert carries no subject of its own.
	DefaultEmailSubject = "breckr alert"
)

// WebhookSource identifies the sender in the generic webhook payload, so a
// receiver handling several sources can tell which one this is.
const WebhookSource = "breckr"

// MaxChannelNameLength keeps names readable in the task form's channel chips.
const MaxChannelNameLength = 60

// --- Notifications ----------------------------------------------------------

// TestNotificationMessage is the body a channel test sends. It says what it is,
// because it lands in a real chat alongside real alerts.
const TestNotificationMessage = "Test notification from breckr. If you can read this, alerts are working."

// TestNotificationSubject is its subject line, for transports that need one.
const TestNotificationSubject = "breckr test notification"

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

// MaxConditions caps how many conditions one task can carry.
//
// A run waits for each selector in turn, so the real ceiling is on how long a
// single run can take before the run timeout does the capping for us -- badly,
// as a generic timeout that names no selector. A task that needs more than this
// is two tasks.
const MaxConditions = 10

// MessagePlaceholders are the unindexed placeholders a message template may
// reference. {{value}} and {{raw}} are the first condition's.
var MessagePlaceholders = []string{"value", "raw", "url", "name"}

// MessagePlaceholderPattern captures {{name}}, tolerating inner whitespace.
var MessagePlaceholderPattern = regexp.MustCompile(`\{\{\s*(\w+)\s*\}\}`)

// IndexedPlaceholderPattern splits {{value2}} into "value" and "2".
//
// Indexed placeholders are validated against how many conditions the task
// actually has, so {{value3}} on a two-condition task is a save-time error
// rather than a literal "{{value3}}" arriving in the one message you most
// wanted to be right.
var IndexedPlaceholderPattern = regexp.MustCompile(`^(value|raw)([1-9][0-9]*)$`)
