package runner

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"breckr-server/internal/browser"
	"breckr-server/internal/config"
	"breckr-server/internal/crypto"
	"breckr-server/internal/events"
	"breckr-server/internal/migrations"
	"breckr-server/internal/notifier"
	"breckr-server/internal/store"
	"breckr-server/internal/types"
)

/*
The whole notification path with nothing faked but the page: the real dispatcher,
the real channel store, real encryption, and real HTTP transports pointed at
httptest servers.

The unit tests above pin the runner against a forced outcome, and
notifier/dispatcher_test.go pins the aggregation. This is what proves the two
halves are wired to each other -- that a task's channels are actually resolved,
actually sent to, and actually recorded.
*/

// recorder is a channel endpoint that counts what it received.
type recorder struct {
	mu       sync.Mutex
	messages []string
	server   *httptest.Server
}

func newRecorder(t *testing.T) *recorder {
	t.Helper()

	rec := &recorder{}
	rec.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)

		var payload map[string]any
		_ = json.Unmarshal(body, &payload)

		rec.mu.Lock()
		message, _ := payload["message"].(string)
		rec.messages = append(rec.messages, message)
		rec.mu.Unlock()

		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(rec.server.Close)

	return rec
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.messages)
}

// liveHarness is newHarness with the real dispatcher instead of the fake.
type liveHarness struct {
	runner   *Runner
	tasks    *store.SQLiteTaskStore
	channels *store.SQLiteChannelStore
	// db is held so a test can corrupt a row directly, which is the only way to
	// reproduce a replaced key file.
	db *sql.DB
}

func newLiveHarness(t *testing.T) *liveHarness {
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
	quiet := log.New(io.Discard, "", 0)

	return &liveHarness{
		runner: New(tasks, runs, channels, browser.NewPool(cfg),
			notifier.NewDispatcher(channels, quiet), events.New(), quiet),
		tasks:    tasks,
		channels: channels,
		db:       db,
	}
}

// channel saves a real webhook channel pointed at url.
func (h *liveHarness) channel(t *testing.T, name, url string) string {
	t.Helper()

	config, err := json.Marshal(map[string]string{"url": url})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	created, err := h.channels.CreateChannel(store.CreateChannelInput{
		ID: name, Name: name, Type: types.ChannelWebhook, Config: config, Enabled: true,
	})
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	return created.ID
}

// task saves a browserless task linked to the given channels, and returns the
// definition plus a pointer that controls whether its condition matches.
func (h *liveHarness) task(t *testing.T, channelIDs []string) (*types.ResolvedTask, *float64) {
	t.Helper()

	const id = "price-check"
	value := 500.0

	if _, err := h.tasks.CreateTask(store.CreateTaskInput{
		ID: id, Name: "Price check", CronExpr: "*/1 * * * *",
		Spec: &types.TaskSpec{
			URL: "https://example.com", Match: types.MatchAll,
			Conditions: []types.Condition{{
				Selector: "#value", Extract: types.ExtractNumber,
				Operator: types.OpLT, Value: "100",
			}},
		},
		Enabled:    true,
		ChannelIDs: channelIDs,
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
			return &types.TaskResult{Value: value, Raw: "x", URL: "https://example.com"}, nil
		},
		Condition: func(result *types.TaskResult) (bool, error) {
			return result.Value.(float64) < 100, nil
		},
		Notify: func(*types.TaskResult) string { return "price dropped" },
	}, &value
}

func (h *liveHarness) armed(t *testing.T, id string) bool {
	t.Helper()

	task, err := h.tasks.GetTask(id)
	if err != nil || task == nil {
		t.Fatalf("GetTask = %+v, %v", task, err)
	}
	return task.ConditionMet
}

func TestAnAlertReachesEveryAttachedChannel(t *testing.T) {
	h := newLiveHarness(t)

	first := newRecorder(t)
	second := newRecorder(t)
	ids := []string{
		h.channel(t, "first", first.server.URL),
		h.channel(t, "second", second.server.URL),
	}

	definition, value := h.task(t, ids)
	*value = 50

	outcome := h.runner.RunTask(context.Background(), definition, types.TriggerCron)

	if !outcome.Notified {
		t.Fatalf("outcome = %+v, want notified", outcome)
	}
	if first.count() != 1 || second.count() != 1 {
		t.Fatalf("first got %d, second got %d -- want one each",
			first.count(), second.count())
	}
	if !h.armed(t, definition.ID) {
		t.Fatal("a delivered alert must arm the trigger")
	}

	attempts, err := h.channels.ListAttempts(outcome.RunID)
	if err != nil {
		t.Fatalf("ListAttempts: %v", err)
	}
	if len(attempts) != 2 {
		t.Fatalf("got %d attempts, want one row per channel", len(attempts))
	}
	for _, attempt := range attempts {
		if attempt.Status != types.NotificationSent {
			t.Fatalf("%s status = %q, want sent", attempt.ChannelName, attempt.Status)
		}
		// What each channel actually received, so a failure is answerable after
		// the fact rather than only from whatever stdout the process had.
		if attempt.Message == nil || *attempt.Message != "price dropped" {
			t.Fatalf("%s message = %v, want the alert body",
				attempt.ChannelName, attempt.Message)
		}
	}
}

// The decision from the plan: one success is enough, so the working channel is
// not re-alerted just because another failed.
func TestAPartialFailureStillArmsAndIsRecorded(t *testing.T) {
	h := newLiveHarness(t)

	working := newRecorder(t)
	ids := []string{
		h.channel(t, "working", working.server.URL),
		// A port nothing listens on.
		h.channel(t, "broken", "http://127.0.0.1:1/nope"),
	}

	definition, value := h.task(t, ids)
	*value = 50

	outcome := h.runner.RunTask(context.Background(), definition, types.TriggerCron)

	if !outcome.Notified {
		t.Fatalf("outcome = %+v, want notified -- one channel got through", outcome)
	}
	if !h.armed(t, definition.ID) {
		t.Fatal("armed on any delivery, so the working channel is not re-alerted")
	}

	attempts, err := h.channels.ListAttempts(outcome.RunID)
	if err != nil {
		t.Fatalf("ListAttempts: %v", err)
	}

	statuses := map[string]types.NotificationReason{}
	details := map[string]string{}
	for _, attempt := range attempts {
		statuses[attempt.ChannelName] = attempt.Status
		if attempt.Detail != nil {
			details[attempt.ChannelName] = *attempt.Detail
		}
	}

	if statuses["working"] != types.NotificationSent {
		t.Fatalf("working = %q, want sent", statuses["working"])
	}
	if statuses["broken"] != types.NotificationError {
		t.Fatalf("broken = %q, want error", statuses["broken"])
	}
	// Delivered overall, but the broken channel must still be visible or it stays
	// broken forever without anyone noticing.
	if details["broken"] == "" {
		t.Fatal("the failed channel must record why")
	}

	// A second run while the condition holds must not re-alert.
	*value = 40
	h.runner.RunTask(context.Background(), definition, types.TriggerCron)
	if working.count() != 1 {
		t.Fatalf("working got %d alerts, want 1 -- a held condition must not re-alert",
			working.count())
	}
}

// Nothing got through, so the alert is still owed and the next run must retry it.
func TestEveryChannelFailingLeavesTheAlertOwed(t *testing.T) {
	h := newLiveHarness(t)

	ids := []string{
		h.channel(t, "one", "http://127.0.0.1:1/nope"),
		h.channel(t, "two", "http://127.0.0.1:1/nope"),
	}

	definition, value := h.task(t, ids)
	*value = 50

	outcome := h.runner.RunTask(context.Background(), definition, types.TriggerCron)

	if outcome.Notified {
		t.Fatalf("outcome = %+v, want not notified", outcome)
	}
	if h.armed(t, definition.ID) {
		t.Fatal("with every channel failing the trigger must stay disarmed to retry")
	}

	// The retry: now one channel works, and the alert that was owed arrives.
	working := newRecorder(t)
	url, err := json.Marshal(map[string]string{"url": working.server.URL})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if _, err := h.channels.UpdateChannel("one", store.UpdateChannelInput{Config: url}); err != nil {
		t.Fatalf("UpdateChannel: %v", err)
	}

	h.runner.RunTask(context.Background(), definition, types.TriggerCron)

	if working.count() != 1 {
		t.Fatalf("repaired channel got %d alerts, want the owed one to be retried",
			working.count())
	}
	if !h.armed(t, definition.ID) {
		t.Fatal("the retry succeeded, so the trigger must now be armed")
	}
}

// A task nobody attached a channel to is "disabled", not "error": nothing is
// owed, so it arms as if sent and dedup behaves the same as in production.
func TestATaskWithNoChannelsArmsWithoutRetrying(t *testing.T) {
	h := newLiveHarness(t)

	definition, value := h.task(t, nil)
	*value = 50

	outcome := h.runner.RunTask(context.Background(), definition, types.TriggerCron)

	if outcome.Notified {
		t.Fatalf("outcome = %+v, want not notified", outcome)
	}
	if !h.armed(t, definition.ID) {
		t.Fatal("nothing was owed, so the trigger arms as if sent")
	}

	attempts, err := h.channels.ListAttempts(outcome.RunID)
	if err != nil {
		t.Fatalf("ListAttempts: %v", err)
	}
	if len(attempts) != 0 {
		t.Fatalf("got %d attempts, want none -- there was nowhere to send", len(attempts))
	}
}

// Muting is not unlinking: the link survives, but the send skips it.
func TestAMutedChannelIsSkipped(t *testing.T) {
	h := newLiveHarness(t)

	muted := newRecorder(t)
	working := newRecorder(t)
	ids := []string{
		h.channel(t, "muted", muted.server.URL),
		h.channel(t, "working", working.server.URL),
	}

	definition, value := h.task(t, ids)

	disabled := false
	if _, err := h.channels.UpdateChannel("muted", store.UpdateChannelInput{
		Enabled: &disabled,
	}); err != nil {
		t.Fatalf("UpdateChannel: %v", err)
	}

	*value = 50
	h.runner.RunTask(context.Background(), definition, types.TriggerCron)

	if muted.count() != 0 {
		t.Fatalf("muted channel got %d alerts, want none", muted.count())
	}
	if working.count() != 1 {
		t.Fatalf("working channel got %d alerts, want 1", working.count())
	}

	// The link is still saved, so re-enabling restores delivery with no edit to
	// the task.
	saved, err := h.channels.ListChannelIDsForTask(definition.ID)
	if err != nil {
		t.Fatalf("ListChannelIDsForTask: %v", err)
	}
	if len(saved) != 2 {
		t.Fatalf("got %v, want the muted channel still linked", saved)
	}
}

// Editing a task's channels has to take effect on the next alert. The scheduler
// caches compiled definitions, so this is the case that would break if channel
// ids were baked into one instead of looked up at send time.
func TestChangingChannelsTakesEffectWithoutRecompiling(t *testing.T) {
	h := newLiveHarness(t)

	original := newRecorder(t)
	replacement := newRecorder(t)
	originalID := h.channel(t, "original", original.server.URL)
	replacementID := h.channel(t, "replacement", replacement.server.URL)

	definition, value := h.task(t, []string{originalID})

	*value = 50
	h.runner.RunTask(context.Background(), definition, types.TriggerCron)
	if original.count() != 1 {
		t.Fatalf("original got %d, want 1", original.count())
	}

	// Swap the channel and re-arm, holding on to the same definition the
	// scheduler would have cached.
	if err := h.channels.SetTaskChannels(definition.ID, []string{replacementID}); err != nil {
		t.Fatalf("SetTaskChannels: %v", err)
	}
	*value = 500
	h.runner.RunTask(context.Background(), definition, types.TriggerCron)
	*value = 50
	h.runner.RunTask(context.Background(), definition, types.TriggerCron)

	if replacement.count() != 1 {
		t.Fatalf("replacement got %d, want the alert to follow the new link",
			replacement.count())
	}
	if original.count() != 1 {
		t.Fatalf("original got %d, want it to stop receiving after being unlinked",
			original.count())
	}
}

// A stale key file leaves rows that cannot be decrypted. That must surface as a
// real failure, not a silently skipped channel.
func TestABrokenChannelFailsRatherThanBeingSkipped(t *testing.T) {
	h := newLiveHarness(t)

	id := h.channel(t, "stale", "http://127.0.0.1:1/nope")

	// Simulates the key file having been replaced: the row is intact but no
	// longer decrypts.
	_, err := h.db.Exec(
		`UPDATE channels SET config_encrypted = 'no-longer-valid' WHERE id = ?`, id)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}

	definition, value := h.task(t, []string{id})
	*value = 50

	outcome := h.runner.RunTask(context.Background(), definition, types.TriggerCron)

	if outcome.Notified {
		t.Fatalf("outcome = %+v, want not notified", outcome)
	}

	attempts, err := h.channels.ListAttempts(outcome.RunID)
	if err != nil {
		t.Fatalf("ListAttempts: %v", err)
	}
	if len(attempts) != 1 || attempts[0].Status != types.NotificationError {
		t.Fatalf("attempts = %+v, want one error row", attempts)
	}
	if attempts[0].Detail == nil || !strings.Contains(*attempts[0].Detail, "stale") {
		t.Fatal("the failure must name the channel to fix")
	}
}
