package scheduler

import (
	"io"
	"log"
	"path/filepath"
	"testing"
	"time"

	"breckr-server/internal/config"
	"breckr-server/internal/executor"
	"breckr-server/internal/migrations"
	"breckr-server/internal/store"
	"breckr-server/internal/types"
)

/*
The registry has to be mutable at run time: a task saved at 10:05 must start
firing without a restart, and toggling one must not lose its compiled
definition -- "run now" works on a disabled task, and that needs the definition.
*/

func newRegistry(t *testing.T) (*Registry, *store.SQLiteTaskStore) {
	t.Helper()

	cfg := &config.Config{
		Database: config.DatabaseConfig{Path: filepath.Join(t.TempDir(), "test.db")},
		Runtime:  config.RuntimeConfig{Location: time.UTC},
	}

	db, err := store.Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := store.MigrateFS(db, migrations.FS, "."); err != nil {
		t.Fatalf("MigrateFS: %v", err)
	}

	tasks := store.NewSQLiteTaskStore(db)
	quiet := log.New(io.Discard, "", 0)

	registry := New(cfg, tasks, executor.New(nil, 30*time.Second), quiet)

	// Register refuses to arm anything before a handler is installed, so every
	// test goes through ScheduleAll first -- which is also what boot does.
	if err := registry.ScheduleAll(func(*types.ResolvedTask, types.TriggerSource) {}); err != nil {
		t.Fatalf("ScheduleAll: %v", err)
	}
	t.Cleanup(registry.Stop)

	return registry, tasks
}

func storeTask(t *testing.T, tasks *store.SQLiteTaskStore, id string, enabled bool) *types.Task {
	t.Helper()

	task, err := tasks.CreateTask(store.CreateTaskInput{
		ID:       id,
		Name:     "Task " + id,
		CronExpr: "*/15 * * * *",
		Spec: &types.TaskSpec{
			URL: "https://example.com", Match: types.MatchAll,
			Conditions: []types.Condition{{
				Selector: ".price", Extract: types.ExtractNumber,
				Operator: types.OpLT, Value: "100",
			}},
		},
		Enabled: enabled,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	return task
}

func TestRegisterArmsAnEnabledTask(t *testing.T) {
	registry, tasks := newRegistry(t)
	task := storeTask(t, tasks, "price-check", true)

	if !registry.Register(task) {
		t.Fatal("Register should accept a task with a usable spec")
	}
	if registry.GetDefinition("price-check") == nil {
		t.Fatal("the compiled definition should be available")
	}
	if registry.GetNextRun("price-check") == nil {
		t.Fatal("an enabled task should report a next run")
	}
}

// A disabled task keeps its compiled definition but is not armed. Losing the
// definition would break "run now", which deliberately works while disabled.
func TestADisabledTaskIsRegisteredButNotArmed(t *testing.T) {
	registry, tasks := newRegistry(t)
	task := storeTask(t, tasks, "price-check", false)

	if !registry.Register(task) {
		t.Fatal("Register should accept a disabled task")
	}
	if registry.GetDefinition("price-check") == nil {
		t.Fatal("a disabled task must keep its definition -- run-now needs it")
	}
	if registry.GetNextRun("price-check") != nil {
		t.Fatal("a disabled task has no next run")
	}
}

// A row with no usable spec -- legacy, or corrupt JSON -- keeps its history but
// can never run. It is logged and skipped, not fatal: refusing to boot would
// lock the user out of the only UI that could repair it.
func TestRegisterRefusesATaskWithNoSpec(t *testing.T) {
	registry, _ := newRegistry(t)

	orphan := &types.Task{ID: "legacy", Name: "Legacy", CronExpr: "* * * * *", Enabled: true}

	if registry.Register(orphan) {
		t.Fatal("Register should refuse a spec-less task")
	}
	if registry.GetDefinition("legacy") != nil {
		t.Fatal("a refused task must not land in the registry")
	}
}

func TestRegisterIsIdempotent(t *testing.T) {
	registry, tasks := newRegistry(t)
	task := storeTask(t, tasks, "price-check", true)

	registry.Register(task)
	registry.Register(task)

	if ids := registry.ListIDs(); len(ids) != 1 {
		t.Fatalf("registering twice left %d entries: %v", len(ids), ids)
	}
}

func TestRescheduleReplacesTheEntry(t *testing.T) {
	registry, tasks := newRegistry(t)
	task := storeTask(t, tasks, "price-check", true)
	registry.Register(task)

	before := registry.GetNextRun("price-check")

	// Daily at midnight: the next fire has to move well away from a
	// quarter-hourly one.
	hourly := "0 0 * * *"
	if _, err := tasks.UpdateTask("price-check", store.UpdateTaskInput{CronExpr: &hourly}); err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}
	updated, _ := tasks.GetTask("price-check")

	if !registry.Reschedule(updated) {
		t.Fatal("Reschedule should accept the updated task")
	}

	after := registry.GetNextRun("price-check")
	if before == nil || after == nil || *before == *after {
		t.Fatalf("the next run should move: before=%v after=%v", before, after)
	}
	if ids := registry.ListIDs(); len(ids) != 1 {
		t.Fatalf("rescheduling left %d entries: %v", len(ids), ids)
	}
}

func TestUnregisterRemovesTheEntry(t *testing.T) {
	registry, tasks := newRegistry(t)
	registry.Register(storeTask(t, tasks, "price-check", true))

	registry.Unregister("price-check")

	if registry.GetDefinition("price-check") != nil {
		t.Fatal("the definition should be gone")
	}
	if len(registry.ListIDs()) != 0 {
		t.Fatal("the entry should be gone")
	}

	// Safe for an id that was never armed.
	registry.Unregister("never-existed")
}

func TestSetEnabledArmsDisarmsAndPersists(t *testing.T) {
	registry, tasks := newRegistry(t)
	registry.Register(storeTask(t, tasks, "price-check", true))

	if !registry.SetEnabled("price-check", false) {
		t.Fatal("SetEnabled should succeed for a registered task")
	}
	if registry.GetNextRun("price-check") != nil {
		t.Fatal("a disarmed task has no next run")
	}
	stored, _ := tasks.GetTask("price-check")
	if stored.Enabled {
		t.Fatal("SetEnabled must persist to the row")
	}

	if !registry.SetEnabled("price-check", true) {
		t.Fatal("SetEnabled should re-arm")
	}
	if registry.GetNextRun("price-check") == nil {
		t.Fatal("a re-armed task should report a next run")
	}
	stored, _ = tasks.GetTask("price-check")
	if !stored.Enabled {
		t.Fatal("re-enabling must persist too")
	}
}

func TestSetEnabledReportsFalseForAnUnknownTask(t *testing.T) {
	registry, _ := newRegistry(t)

	// This is what makes PATCH answer 409 for a spec-less task rather than
	// pretending the toggle worked.
	if registry.SetEnabled("legacy", true) {
		t.Fatal("SetEnabled should refuse a task that is not registered")
	}
}

func TestScheduleAllSkipsUnusableRowsWithoutFailing(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{Path: filepath.Join(t.TempDir(), "test.db")},
		Runtime:  config.RuntimeConfig{Location: time.UTC},
	}

	db, err := store.Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.MigrateFS(db, migrations.FS, "."); err != nil {
		t.Fatalf("MigrateFS: %v", err)
	}

	tasks := store.NewSQLiteTaskStore(db)
	storeTask(t, tasks, "good", true)
	if _, err := db.Exec(
		`INSERT INTO tasks (id, name, cron_expr) VALUES (?, ?, ?)`, "legacy", "Legacy", "* * * * *",
	); err != nil {
		t.Fatalf("inserting a legacy row: %v", err)
	}

	registry := New(cfg, tasks, executor.New(nil, 30*time.Second), log.New(io.Discard, "", 0))
	t.Cleanup(registry.Stop)

	if err := registry.ScheduleAll(func(*types.ResolvedTask, types.TriggerSource) {}); err != nil {
		t.Fatalf("a corrupt row must not fail the boot: %v", err)
	}

	if registry.GetDefinition("good") == nil {
		t.Fatal("the usable task should be armed")
	}
	if registry.GetDefinition("legacy") != nil {
		t.Fatal("the unusable row should be skipped, not armed")
	}
}
