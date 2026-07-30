package runner

import (
	"context"
	"errors"
	"io"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"breckr-server/internal/browser"
	"breckr-server/internal/config"
	"breckr-server/internal/crypto"
	"breckr-server/internal/migrations"
	"breckr-server/internal/notifier"
	"breckr-server/internal/store"
	"breckr-server/internal/types"
)

/*
The edge-trigger state machine.

The `error` vs `disabled` distinction in particular is easy to "tidy" into a
bug: a failed delivery still owes an alert, while a disabled notifier owes
nothing. These tests exist so a refactor cannot quietly change that.

The Browser here is the real browser.Pool, driven through WithoutPage -- it needs
no CDP connection, and using it means the mutex and the run timeout are the
production ones rather than a stand-in that could drift.
*/

// fakeNotifier captures messages and lets each test force the delivery outcome.
//
// It stands in for the whole fan-out: these tests are about what the runner does
// with an aggregate outcome, and the aggregation rule itself is pinned in
// notifier/dispatcher_test.go.
type fakeNotifier struct {
	mu      sync.Mutex
	outcome types.NotificationOutcome
	sent    []string
}

func (f *fakeNotifier) DispatchTask(_ context.Context, _ string, message notifier.Message) notifier.Fanout {
	f.mu.Lock()
	defer f.mu.Unlock()

	// A task with no channels still "reports" the message (it logs it), so count
	// both delivered and disabled as observable alerts.
	if f.outcome.Delivered || f.outcome.Reason == types.NotificationDisabled {
		f.sent = append(f.sent, message.Body)
	}

	fanout := notifier.Fanout{Aggregate: f.outcome}

	// "disabled" means nothing was attached, so there is no per-channel row to
	// write -- mirroring what the real dispatcher returns.
	if f.outcome.Reason != types.NotificationDisabled {
		fanout.Deliveries = []notifier.Delivery{{
			ChannelID:   "channel-1",
			ChannelName: "Test channel",
			ChannelType: types.ChannelTelegram,
			Outcome:     f.outcome,
		}}
	}

	return fanout
}

func (f *fakeNotifier) DispatchChannel(
	_ context.Context,
	_ *store.StoredChannel,
	_ notifier.Message,
) types.NotificationOutcome {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.outcome
}

func (f *fakeNotifier) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

func (f *fakeNotifier) set(outcome types.NotificationOutcome) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.outcome = outcome
}

type harness struct {
	runner   *Runner
	tasks    *store.SQLiteTaskStore
	runs     *store.SQLiteRunStore
	channels *store.SQLiteChannelStore
	notifier *fakeNotifier
}

func newHarness(t *testing.T, outcome types.NotificationOutcome) *harness {
	t.Helper()

	cfg := &config.Config{
		Database: config.DatabaseConfig{Path: filepath.Join(t.TempDir(), "test.db")},
	}

	db, err := store.Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := store.MigrateFS(db, migrations.FS, "."); err != nil {
		t.Fatalf("MigrateFS: %v", err)
	}

	key, err := crypto.LoadOrCreateKey(filepath.Join(t.TempDir(), "secret.key"))
	if err != nil {
		t.Fatalf("LoadOrCreateKey: %v", err)
	}
	cipher, err := crypto.New(key)
	if err != nil {
		t.Fatalf("crypto.New: %v", err)
	}

	tasks := store.NewSQLiteTaskStore(db)
	runs := store.NewSQLiteRunStore(db)
	channels := store.NewSQLiteChannelStore(db, cipher)
	notify := &fakeNotifier{outcome: outcome}
	quiet := log.New(io.Discard, "", 0)

	return &harness{
		runner:   New(tasks, runs, channels, browser.NewPool(cfg), notify, quiet),
		tasks:    tasks,
		runs:     runs,
		channels: channels,
		notifier: notify,
	}
}

// task builds a browserless task with its own row.
//
// The runner reads edge-trigger state off the task row, so one has to exist.
// Its stored spec is irrelevant here -- these tests drive the ResolvedTask
// directly rather than going through the executor.
func (h *harness) task(t *testing.T, value *float64) *types.ResolvedTask {
	t.Helper()

	const id = "price-check"

	if _, err := h.tasks.CreateTask(store.CreateTaskInput{
		ID:       id,
		Name:     "Price check",
		CronExpr: "*/1 * * * *",
		Spec: &types.TaskSpec{
			URL: "https://example.com", Selector: "#value",
			Extract: types.ExtractNumber, Operator: types.OpLT, Value: "100",
		},
		Enabled: true,
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	return &types.ResolvedTask{
		ID:           id,
		Name:         "Price check",
		Cron:         "*/1 * * * *",
		Timeout:      5 * time.Second,
		NeedsBrowser: false,
		Run: func(types.Page) (*types.TaskResult, error) {
			return &types.TaskResult{Value: *value, Raw: "x", URL: "https://example.com"}, nil
		},
		Condition: func(result *types.TaskResult) (bool, error) {
			return result.Value.(float64) < 100, nil
		},
		Notify: func(result *types.TaskResult) string {
			return "value=" + strings.TrimSpace(result.Raw)
		},
	}
}

func (h *harness) run(t *testing.T, definition *types.ResolvedTask) types.RunOutcome {
	t.Helper()
	return h.runner.RunTask(context.Background(), definition, types.TriggerCron)
}

func (h *harness) isArmed(t *testing.T, id string) bool {
	t.Helper()

	task, err := h.tasks.GetTask(id)
	if err != nil || task == nil {
		t.Fatalf("GetTask = %+v, %v", task, err)
	}
	return task.ConditionMet
}

func TestNotifiesOnceWhileTheConditionHolds(t *testing.T) {
	h := newHarness(t, types.NotificationOutcome{Delivered: true, Reason: types.NotificationSent})
	value := 500.0
	definition := h.task(t, &value)

	value = 50
	h.run(t, definition)
	if h.notifier.count() != 1 {
		t.Fatalf("first match should alert, sent %d", h.notifier.count())
	}

	value = 40
	h.run(t, definition)
	value = 30
	h.run(t, definition)

	if h.notifier.count() != 1 {
		t.Fatalf("a held condition must not re-alert, sent %d", h.notifier.count())
	}
	if !h.isArmed(t, definition.ID) {
		t.Fatal("state stays armed while the condition holds")
	}
}

func TestReArmsAfterTheConditionClearsThenFiresAgain(t *testing.T) {
	h := newHarness(t, types.NotificationOutcome{Delivered: true, Reason: types.NotificationSent})
	value := 500.0
	definition := h.task(t, &value)

	value = 50
	h.run(t, definition)
	if h.notifier.count() != 1 {
		t.Fatalf("sent %d, want 1", h.notifier.count())
	}

	value = 500
	h.run(t, definition)
	if h.notifier.count() != 1 {
		t.Fatalf("clearing must not alert, sent %d", h.notifier.count())
	}
	if h.isArmed(t, definition.ID) {
		t.Fatal("clearing re-arms the trigger")
	}

	value = 10
	h.run(t, definition)
	if h.notifier.count() != 2 {
		t.Fatalf("a fresh transition alerts again, sent %d", h.notifier.count())
	}
}

func TestAFailedDeliveryIsRetriedOnTheNextRun(t *testing.T) {
	h := newHarness(t, types.NotificationOutcome{Delivered: false, Reason: types.NotificationError})
	value := 20.0
	definition := h.task(t, &value)

	first := h.run(t, definition)

	if first.Notified {
		t.Fatal("the run must not claim it notified")
	}
	if h.isArmed(t, definition.ID) {
		t.Fatal("state must stay disarmed so the alert is retried")
	}

	h.notifier.set(types.NotificationOutcome{Delivered: true, Reason: types.NotificationSent})
	h.run(t, definition)

	if h.notifier.count() != 1 {
		t.Fatalf("the retry delivers, sent %d", h.notifier.count())
	}
	if !h.isArmed(t, definition.ID) {
		t.Fatal("a delivered retry arms the trigger")
	}
}

func TestADisabledNotifierDedupsExactlyLikeAWorkingOne(t *testing.T) {
	h := newHarness(t, types.NotificationOutcome{Delivered: false, Reason: types.NotificationDisabled})
	value := 5.0
	definition := h.task(t, &value)

	h.run(t, definition)
	if h.notifier.count() != 1 {
		t.Fatalf("logs the first match, sent %d", h.notifier.count())
	}

	value = 4
	h.run(t, definition)
	value = 3
	h.run(t, definition)

	if h.notifier.count() != 1 {
		t.Fatalf("nothing is owed, so state advances and dedup holds, sent %d", h.notifier.count())
	}
}

func TestAFailedRunLeavesTheArmedStateUntouched(t *testing.T) {
	h := newHarness(t, types.NotificationOutcome{Delivered: true, Reason: types.NotificationSent})
	value := 50.0
	definition := h.task(t, &value)

	h.run(t, definition)
	armedBefore := h.isArmed(t, definition.ID)

	exploding := *definition
	exploding.Run = func(types.Page) (*types.TaskResult, error) {
		return nil, errors.New("browser exploded")
	}

	outcome := h.run(t, &exploding)

	if outcome.Status != types.RunStatusFailed {
		t.Fatalf("status = %q, want failed", outcome.Status)
	}
	if h.isArmed(t, definition.ID) != armedBefore {
		t.Fatal("an error is not evidence the condition cleared")
	}
}

func TestAFailingConditionFailsTheRunButKeepsTheResult(t *testing.T) {
	h := newHarness(t, types.NotificationOutcome{Delivered: true, Reason: types.NotificationSent})
	value := 50.0
	definition := h.task(t, &value)
	definition.Condition = func(*types.TaskResult) (bool, error) {
		return false, errors.New("bad condition")
	}

	outcome := h.run(t, definition)

	row, err := h.runs.GetRun(outcome.RunID)
	if err != nil || row == nil {
		t.Fatalf("GetRun = %+v, %v", row, err)
	}

	if outcome.Status != types.RunStatusFailed {
		t.Fatalf("status = %q, want failed", outcome.Status)
	}
	if row.ResultSummary == nil {
		t.Fatal("the extracted result is retained -- the extraction worked")
	}
	if row.Error == nil || !strings.Contains(*row.Error, "condition failed") {
		t.Fatalf("error = %v, want it to name the condition", row.Error)
	}
}

func TestARunThatOutlivesItsTimeoutFailsPromptly(t *testing.T) {
	h := newHarness(t, types.NotificationOutcome{Delivered: true, Reason: types.NotificationSent})
	value := 50.0
	definition := h.task(t, &value)
	definition.Timeout = 150 * time.Millisecond
	definition.Run = func(types.Page) (*types.TaskResult, error) {
		time.Sleep(5 * time.Second)
		return &types.TaskResult{Value: 1.0}, nil
	}

	startedAt := time.Now()
	outcome := h.run(t, definition)
	elapsed := time.Since(startedAt)

	if outcome.Status != types.RunStatusFailed {
		t.Fatalf("status = %q, want failed", outcome.Status)
	}
	if elapsed > 1500*time.Millisecond {
		t.Fatalf("should have given up promptly, took %s", elapsed)
	}

	row, _ := h.runs.GetRun(outcome.RunID)
	if row.Error == nil || !strings.Contains(*row.Error, "timed out") {
		t.Fatalf("error = %v, want a timeout", row.Error)
	}
}

func TestRunRowsExposeRealBooleans(t *testing.T) {
	h := newHarness(t, types.NotificationOutcome{Delivered: true, Reason: types.NotificationSent})
	value := 5.0
	definition := h.task(t, &value)

	outcome := h.run(t, definition)

	row, err := h.runs.GetRun(outcome.RunID)
	if err != nil || row == nil {
		t.Fatalf("GetRun = %+v, %v", row, err)
	}
	if !row.ConditionMet || !row.Notified {
		t.Fatalf("SQLite 0/1 should convert at the store boundary: %+v", row)
	}
}

/*
The delivery outcome has to survive on the run row, because stdout is not an
answer to "did my alert go out?" a week later.

`notified` alone cannot carry it: a rejected send and a run that owed no alert
both leave it false, and those are opposite situations -- one is a fault to fix,
the other is normal operation.
*/

// row fetches the run these tests just wrote.
func (h *harness) row(t *testing.T, outcome types.RunOutcome) *types.Run {
	t.Helper()

	row, err := h.runs.GetRun(outcome.RunID)
	if err != nil || row == nil {
		t.Fatalf("GetRun = %+v, %v", row, err)
	}
	return row
}

func TestADeliveredAlertRecordsWhatWasSent(t *testing.T) {
	h := newHarness(t, types.NotificationOutcome{Delivered: true, Reason: types.NotificationSent})
	value := 5.0
	definition := h.task(t, &value)

	row := h.row(t, h.run(t, definition))

	if row.NotificationStatus == nil || *row.NotificationStatus != types.NotificationSent {
		t.Fatalf("status = %v, want sent", row.NotificationStatus)
	}
	if row.NotificationDetail != nil {
		t.Fatalf("detail = %v, want nil on a delivered alert", *row.NotificationDetail)
	}
	// The body is the actual evidence: it is what the chat should contain.
	if row.NotificationMessage == nil || *row.NotificationMessage != "value=x" {
		t.Fatalf("message = %v, want the rendered body", row.NotificationMessage)
	}
}

func TestARejectedAlertRecordsWhyItFailed(t *testing.T) {
	h := newHarness(t, types.NotificationOutcome{
		Delivered: false,
		Reason:    types.NotificationError,
		Detail:    "Telegram rejected the notification (400): chat not found",
	})
	value := 5.0
	definition := h.task(t, &value)

	row := h.row(t, h.run(t, definition))

	if row.NotificationStatus == nil || *row.NotificationStatus != types.NotificationError {
		t.Fatalf("status = %v, want error", row.NotificationStatus)
	}
	if row.NotificationDetail == nil || !strings.Contains(*row.NotificationDetail, "chat not found") {
		t.Fatalf("detail = %v, want the transport's own reason", row.NotificationDetail)
	}
	// The body is kept even though nothing arrived -- it is what the retry owes.
	if row.NotificationMessage == nil {
		t.Fatal("the undelivered body must still be recorded")
	}
	if row.Notified {
		t.Fatal("a rejected alert did not notify")
	}
}

func TestADisabledNotifierIsRecordedAsDisabledNotAsFailure(t *testing.T) {
	h := newHarness(t, types.NotificationOutcome{
		Delivered: false,
		Reason:    types.NotificationDisabled,
		Detail:    "Telegram is not configured",
	})
	value := 5.0
	definition := h.task(t, &value)

	row := h.row(t, h.run(t, definition))

	// Distinct from "error" on purpose: nothing is owed and nothing will retry,
	// so this is the state of the install rather than a fault.
	if row.NotificationStatus == nil || *row.NotificationStatus != types.NotificationDisabled {
		t.Fatalf("status = %v, want disabled", row.NotificationStatus)
	}
}

func TestARunThatOwedNoAlertRecordsNoStatus(t *testing.T) {
	h := newHarness(t, types.NotificationOutcome{Delivered: true, Reason: types.NotificationSent})
	value := 500.0
	definition := h.task(t, &value)

	// 500 fails the `< 100` condition, so no alert is due at all.
	row := h.row(t, h.run(t, definition))

	if row.NotificationStatus != nil {
		t.Fatalf("status = %v, want nil -- no alert was owed", *row.NotificationStatus)
	}
	if row.NotificationMessage != nil {
		t.Fatalf("message = %v, want nil -- nothing was composed", *row.NotificationMessage)
	}
}

func TestAHeldConditionOwesNoAlertOnTheSecondRun(t *testing.T) {
	h := newHarness(t, types.NotificationOutcome{Delivered: true, Reason: types.NotificationSent})
	value := 50.0
	definition := h.task(t, &value)

	first := h.row(t, h.run(t, definition))
	value = 40
	second := h.row(t, h.run(t, definition))

	if first.NotificationStatus == nil {
		t.Fatal("the transition alerts")
	}
	// The condition still holds, so dedup suppressed the alert. That is not a
	// delivery failure and must not read as one.
	if second.NotificationStatus != nil {
		t.Fatalf("status = %v, want nil while the condition holds", *second.NotificationStatus)
	}
}
