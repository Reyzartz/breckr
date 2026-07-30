package store

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"breckr-server/internal/crypto"
	"breckr-server/internal/types"
)

/*
Two things here are easy to get wrong and expensive when wrong: what the database
file actually contains (a plaintext token would defeat the whole crypto package),
and what survives a delete (run history has to stay readable after the channel it
was sent through is gone).
*/

func newChannelStore(t *testing.T, db *sql.DB) *SQLiteChannelStore {
	t.Helper()

	key, err := crypto.LoadOrCreateKey(filepath.Join(t.TempDir(), "secret.key"))
	if err != nil {
		t.Fatalf("LoadOrCreateKey: %v", err)
	}
	cipher, err := crypto.New(key)
	if err != nil {
		t.Fatalf("crypto.New: %v", err)
	}

	return NewSQLiteChannelStore(db, cipher)
}

func newChannel(t *testing.T, channels *SQLiteChannelStore, id, name string) *StoredChannel {
	t.Helper()

	config, err := json.Marshal(map[string]string{"token": "123:secret-token", "chat_id": "42"})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	channel, err := channels.CreateChannel(CreateChannelInput{
		ID:      id,
		Name:    name,
		Type:    types.ChannelTelegram,
		Config:  config,
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	return channel
}

func TestAChannelRoundTripsThroughTheStore(t *testing.T) {
	db := newTestDB(t)
	channels := newChannelStore(t, db)

	created := newChannel(t, channels, "c1", "My Telegram")

	if created.Name != "My Telegram" || created.Type != types.ChannelTelegram {
		t.Fatalf("created = %+v, want the submitted name and type", created.Channel)
	}
	if created.Broken {
		t.Fatal("a channel written and read back must not be broken")
	}

	var config map[string]string
	if err := json.Unmarshal(created.Config, &config); err != nil {
		t.Fatalf("stored config did not decrypt into JSON: %v", err)
	}
	if config["token"] != "123:secret-token" {
		t.Fatalf("token = %q, want it back verbatim", config["token"])
	}
}

// The whole reason the crypto package exists: a leaked .db must not be a leaked
// token.
func TestTheStoredColumnHoldsNoPlaintext(t *testing.T) {
	db := newTestDB(t)
	channels := newChannelStore(t, db)

	newChannel(t, channels, "c1", "My Telegram")

	var stored string
	err := db.QueryRow(`SELECT config_encrypted FROM channels WHERE id = 'c1'`).Scan(&stored)
	if err != nil {
		t.Fatalf("QueryRow: %v", err)
	}

	if stored == "" {
		t.Fatal("nothing was stored")
	}
	for _, secret := range []string{"secret-token", "123:", "chat_id"} {
		if strings.Contains(stored, secret) {
			t.Fatalf("the column contains %q in the clear: %s", secret, stored)
		}
	}
}

func TestGetChannelReturnsNilForAnUnknownID(t *testing.T) {
	db := newTestDB(t)
	channels := newChannelStore(t, db)

	channel, err := channels.GetChannel("nope")
	if err != nil {
		t.Fatalf("GetChannel: %v", err)
	}
	if channel != nil {
		t.Fatalf("channel = %+v, want nil for an unknown id", channel)
	}
}

// A channel whose key no longer matches has to keep its identity: the dashboard
// needs to say which one to re-enter, and one bad row must not fail the list that
// is the only place to fix it.
func TestAnUndecryptableChannelIsBrokenNotAnError(t *testing.T) {
	db := newTestDB(t)
	channels := newChannelStore(t, db)

	newChannel(t, channels, "c1", "My Telegram")

	// Simulates a replaced key file.
	if _, err := db.Exec(`UPDATE channels SET config_encrypted = 'not-real-ciphertext' WHERE id = 'c1'`); err != nil {
		t.Fatalf("Exec: %v", err)
	}

	listed, err := channels.ListChannels()
	if err != nil {
		t.Fatalf("ListChannels: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("got %d channels, want the broken one still listed", len(listed))
	}
	if !listed[0].Broken {
		t.Fatal("an undecryptable channel must be flagged broken")
	}
	if listed[0].Name != "My Telegram" {
		t.Fatalf("name = %q, want it preserved so the user knows which to fix", listed[0].Name)
	}
}

func TestUpdatingAChannelLeavesUnsetFieldsAlone(t *testing.T) {
	db := newTestDB(t)
	channels := newChannelStore(t, db)

	newChannel(t, channels, "c1", "Old name")

	renamed := "New name"
	updated, err := channels.UpdateChannel("c1", UpdateChannelInput{Name: &renamed})
	if err != nil {
		t.Fatalf("UpdateChannel: %v", err)
	}

	if updated.Name != "New name" {
		t.Fatalf("name = %q, want the patched value", updated.Name)
	}
	// Renaming must not disturb the credential -- the dashboard never sends it
	// back, so losing it here would silently break the channel.
	var config map[string]string
	if err := json.Unmarshal(updated.Config, &config); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if config["token"] != "123:secret-token" {
		t.Fatalf("token = %q, want it untouched by a rename", config["token"])
	}
}

func TestDuplicateNamesAreRejected(t *testing.T) {
	db := newTestDB(t)
	channels := newChannelStore(t, db)

	newChannel(t, channels, "c1", "Shared name")

	config, _ := json.Marshal(map[string]string{"token": "t", "chat_id": "1"})
	_, err := channels.CreateChannel(CreateChannelInput{
		ID: "c2", Name: "Shared name", Type: types.ChannelTelegram, Config: config, Enabled: true,
	})
	if err == nil {
		t.Fatal("two channels with the same name must be rejected")
	}
}

func TestSetTaskChannelsReplacesTheLinksWholesale(t *testing.T) {
	db := newTestDB(t)
	tasks := NewSQLiteTaskStore(db)
	channels := newChannelStore(t, db)

	newTask(t, tasks, "task-1")
	newChannel(t, channels, "c1", "First")
	newChannel(t, channels, "c2", "Second")

	if err := channels.SetTaskChannels("task-1", []string{"c1", "c2"}); err != nil {
		t.Fatalf("SetTaskChannels: %v", err)
	}
	if ids := taskChannelIDs(t, channels, "task-1"); len(ids) != 2 {
		t.Fatalf("got %v, want both channels linked", ids)
	}

	// Replacement, not a merge.
	if err := channels.SetTaskChannels("task-1", []string{"c2"}); err != nil {
		t.Fatalf("SetTaskChannels: %v", err)
	}

	ids := taskChannelIDs(t, channels, "task-1")
	if len(ids) != 1 || ids[0] != "c2" {
		t.Fatalf("got %v, want exactly [c2]", ids)
	}
}

// Only enabled channels deliver: a disabled channel is one the user muted, not
// one they unlinked, so it stays in the saved links but out of the send.
func TestOnlyEnabledChannelsAreDispatchedTo(t *testing.T) {
	db := newTestDB(t)
	tasks := NewSQLiteTaskStore(db)
	channels := newChannelStore(t, db)

	newTask(t, tasks, "task-1")
	newChannel(t, channels, "c1", "Enabled")
	newChannel(t, channels, "c2", "Muted")

	if err := channels.SetTaskChannels("task-1", []string{"c1", "c2"}); err != nil {
		t.Fatalf("SetTaskChannels: %v", err)
	}

	disabled := false
	if _, err := channels.UpdateChannel("c2", UpdateChannelInput{Enabled: &disabled}); err != nil {
		t.Fatalf("UpdateChannel: %v", err)
	}

	forSend, err := channels.ListChannelsForTask("task-1")
	if err != nil {
		t.Fatalf("ListChannelsForTask: %v", err)
	}
	if len(forSend) != 1 || forSend[0].ID != "c1" {
		t.Fatalf("got %d channels, want only the enabled one", len(forSend))
	}

	// But the link itself survives, so re-enabling restores delivery without
	// re-selecting the channel on the task.
	if ids := taskChannelIDs(t, channels, "task-1"); len(ids) != 2 {
		t.Fatalf("got %v, want the muted channel still linked", ids)
	}
}

func TestCreatingATaskWithChannelsWritesBothAtomically(t *testing.T) {
	db := newTestDB(t)
	tasks := NewSQLiteTaskStore(db)
	channels := newChannelStore(t, db)

	newChannel(t, channels, "c1", "First")

	if _, err := tasks.CreateTask(CreateTaskInput{
		ID: "task-1", Name: "Task", CronExpr: "*/15 * * * *",
		Spec: sampleSpec(), Enabled: true, ChannelIDs: []string{"c1"},
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	if ids := taskChannelIDs(t, channels, "task-1"); len(ids) != 1 || ids[0] != "c1" {
		t.Fatalf("got %v, want the link written with the task", ids)
	}
}

// The foreign key is the guard: a task saved against a channel that no longer
// exists must fail rather than persist a link that dispatches nowhere.
func TestCreatingATaskWithAnUnknownChannelFails(t *testing.T) {
	db := newTestDB(t)
	tasks := NewSQLiteTaskStore(db)

	_, err := tasks.CreateTask(CreateTaskInput{
		ID: "task-1", Name: "Task", CronExpr: "*/15 * * * *",
		Spec: sampleSpec(), Enabled: true, ChannelIDs: []string{"ghost"},
	})
	if err == nil {
		t.Fatal("linking to a nonexistent channel must fail")
	}

	// And the task itself must not survive the failed transaction.
	task, err := tasks.GetTask("task-1")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task != nil {
		t.Fatal("the task was committed despite its links failing -- the write was not atomic")
	}
}

func TestDeletingATaskRemovesItsLinks(t *testing.T) {
	db := newTestDB(t)
	tasks := NewSQLiteTaskStore(db)
	channels := newChannelStore(t, db)

	newTask(t, tasks, "task-1")
	newChannel(t, channels, "c1", "First")

	if err := channels.SetTaskChannels("task-1", []string{"c1"}); err != nil {
		t.Fatalf("SetTaskChannels: %v", err)
	}
	if _, err := tasks.DeleteTask("task-1"); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM task_channels`).Scan(&count); err != nil {
		t.Fatalf("QueryRow: %v", err)
	}
	if count != 0 {
		t.Fatalf("got %d links, want them cascaded away with the task", count)
	}
}

// The reason channel_name and channel_type are copies rather than joins: "which
// channel failed" has to stay answerable after that channel is deleted.
func TestAttemptsSurviveTheChannelBeingDeleted(t *testing.T) {
	db := newTestDB(t)
	tasks := NewSQLiteTaskStore(db)
	runs := NewSQLiteRunStore(db)
	channels := newChannelStore(t, db)

	newTask(t, tasks, "task-1")
	newChannel(t, channels, "c1", "Doomed channel")

	runID, err := runs.StartRun(StartRunInput{TaskID: "task-1", TriggerSource: types.TriggerCron})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}

	err = channels.RecordAttempts(runID, []AttemptInput{{
		ChannelID:   "c1",
		ChannelName: "Doomed channel",
		ChannelType: types.ChannelTelegram,
		Status:      types.NotificationSent,
		Message:     "value=42",
	}})
	if err != nil {
		t.Fatalf("RecordAttempts: %v", err)
	}

	if _, err := channels.DeleteChannel("c1"); err != nil {
		t.Fatalf("DeleteChannel: %v", err)
	}

	attempts, err := channels.ListAttempts(runID)
	if err != nil {
		t.Fatalf("ListAttempts: %v", err)
	}
	if len(attempts) != 1 {
		t.Fatalf("got %d attempts, want the history to survive", len(attempts))
	}
	if attempts[0].ChannelID != nil {
		t.Fatalf("channel_id = %v, want null once the channel is gone", *attempts[0].ChannelID)
	}
	if attempts[0].ChannelName != "Doomed channel" {
		t.Fatalf("name = %q, want the copy kept", attempts[0].ChannelName)
	}
	if attempts[0].Message == nil || *attempts[0].Message != "value=42" {
		t.Fatal("the message that went out must survive too")
	}
}

func TestDeletingARunRemovesItsAttempts(t *testing.T) {
	db := newTestDB(t)
	tasks := NewSQLiteTaskStore(db)
	runs := NewSQLiteRunStore(db)
	channels := newChannelStore(t, db)

	newTask(t, tasks, "task-1")
	newChannel(t, channels, "c1", "First")

	runID, err := runs.StartRun(StartRunInput{TaskID: "task-1", TriggerSource: types.TriggerCron})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	err = channels.RecordAttempts(runID, []AttemptInput{{
		ChannelID: "c1", ChannelName: "First",
		ChannelType: types.ChannelTelegram, Status: types.NotificationSent,
	}})
	if err != nil {
		t.Fatalf("RecordAttempts: %v", err)
	}

	// Run history is pruned on a retention sweep; attempts must go with it rather
	// than accumulating forever behind deleted runs.
	if _, err := tasks.DeleteTask("task-1"); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM notification_attempts`).Scan(&count); err != nil {
		t.Fatalf("QueryRow: %v", err)
	}
	if count != 0 {
		t.Fatalf("got %d attempts, want them cascaded away with the run", count)
	}
}

func TestCountEnabledChannelsIgnoresMutedOnes(t *testing.T) {
	db := newTestDB(t)
	channels := newChannelStore(t, db)

	newChannel(t, channels, "c1", "Enabled")
	newChannel(t, channels, "c2", "Muted")

	disabled := false
	if _, err := channels.UpdateChannel("c2", UpdateChannelInput{Enabled: &disabled}); err != nil {
		t.Fatalf("UpdateChannel: %v", err)
	}

	count, err := channels.CountEnabledChannels()
	if err != nil {
		t.Fatalf("CountEnabledChannels: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want only the enabled channel", count)
	}
}

func taskChannelIDs(t *testing.T, channels *SQLiteChannelStore, taskID string) []string {
	t.Helper()

	ids, err := channels.ListChannelIDsForTask(taskID)
	if err != nil {
		t.Fatalf("ListChannelIDsForTask: %v", err)
	}
	return ids
}
