package store

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"breckr-server/internal/config"
	"breckr-server/internal/migrations"
	"breckr-server/internal/types"
)

// newTestDB gives each test its own database file.
//
// The Node suite had to run serially because every file shared one SQLite path;
// a temp directory per test removes that constraint entirely.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()

	cfg := &config.Config{
		Database: config.DatabaseConfig{Path: filepath.Join(t.TempDir(), "test.db")},
	}

	db, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := MigrateFS(db, migrations.FS, "."); err != nil {
		t.Fatalf("MigrateFS: %v", err)
	}

	return db
}

func sampleSpec() *types.TaskSpec {
	return &types.TaskSpec{
		URL:   "https://example.com",
		Match: types.MatchAll,
		Conditions: []types.Condition{{
			Selector: ".price",
			Extract:  types.ExtractNumber,
			Operator: types.OpLT,
			Value:    "100",
		}},
	}
}

func newTask(t *testing.T, tasks *SQLiteTaskStore, id string) *types.Task {
	t.Helper()

	task, err := tasks.CreateTask(CreateTaskInput{
		ID:       id,
		Name:     "Task " + id,
		CronExpr: "*/15 * * * *",
		Spec:     sampleSpec(),
		Enabled:  true,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	return task
}

// --- tasks ------------------------------------------------------------------

func TestTaskStoreRoundTripsASpec(t *testing.T) {
	tasks := NewSQLiteTaskStore(newTestDB(t))

	created := newTask(t, tasks, "price-check")

	if created.Spec == nil {
		t.Fatal("spec came back nil")
	}
	if len(created.Spec.Conditions) != 1 {
		t.Fatalf("conditions did not round trip: %+v", created.Spec)
	}
	if created.Spec.Conditions[0].Selector != ".price" || created.Spec.Conditions[0].Value != "100" {
		t.Fatalf("spec did not round trip: %+v", created.Spec)
	}
	if !created.Enabled || created.ConditionMet {
		t.Fatalf("a new task should be enabled and disarmed: %+v", created)
	}
}

func TestTaskStoreRejectsADuplicateID(t *testing.T) {
	tasks := NewSQLiteTaskStore(newTestDB(t))
	newTask(t, tasks, "price-check")

	if _, err := tasks.CreateTask(CreateTaskInput{
		ID: "price-check", Name: "Again", CronExpr: "* * * * *", Spec: sampleSpec(),
	}); err == nil {
		t.Fatal("the primary key must reject a duplicate id")
	}
}

func TestTaskStorePatchesOnlyWhatIsPresent(t *testing.T) {
	tasks := NewSQLiteTaskStore(newTestDB(t))
	newTask(t, tasks, "price-check")

	renamed := "Renamed"
	updated, err := tasks.UpdateTask("price-check", UpdateTaskInput{Name: &renamed})
	if err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}

	if updated.Name != "Renamed" {
		t.Fatalf("name = %q", updated.Name)
	}
	if updated.CronExpr != "*/15 * * * *" {
		t.Fatalf("cron_expr should be untouched, got %q", updated.CronExpr)
	}
	if updated.Spec == nil || len(updated.Spec.Conditions) != 1 ||
		updated.Spec.Conditions[0].Selector != ".price" {
		t.Fatalf("spec should be untouched, got %+v", updated.Spec)
	}
}

// Editing the spec re-arms the edge-trigger: the persisted condition_met
// describes the *old* condition, and carrying it over would let a stale
// "already alerted" flag swallow the first alert of the new one.
func TestEditingTheSpecReArmsButRenamingDoesNot(t *testing.T) {
	tasks := NewSQLiteTaskStore(newTestDB(t))
	newTask(t, tasks, "price-check")

	if err := tasks.MarkTaskNotified("price-check"); err != nil {
		t.Fatalf("MarkTaskNotified: %v", err)
	}

	renamed := "Renamed"
	afterRename, err := tasks.UpdateTask("price-check", UpdateTaskInput{Name: &renamed})
	if err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}
	if !afterRename.ConditionMet {
		t.Fatal("a rename must not re-arm the edge-trigger")
	}
	if afterRename.LastNotifiedAt == nil {
		t.Fatal("last_notified_at should be stamped after a delivery")
	}

	newSpec := sampleSpec()
	newSpec.Conditions[0].Value = "50"
	afterEdit, err := tasks.UpdateTask("price-check", UpdateTaskInput{Spec: newSpec})
	if err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}
	if afterEdit.ConditionMet {
		t.Fatal("editing the spec must re-arm the edge-trigger")
	}
}

func TestANewTaskDefaultsToTransitionMode(t *testing.T) {
	tasks := NewSQLiteTaskStore(newTestDB(t))

	// newTask names no mode, which is also what a row written before the column
	// existed looks like after the migration.
	created := newTask(t, tasks, "price-check")

	if created.NotifyMode != types.NotifyOnTransition {
		t.Fatalf("notify_mode = %q, want the pre-existing behaviour", created.NotifyMode)
	}
}

// The mode is a column rather than part of the spec precisely so these two can
// be changed independently: editing the spec re-arms, changing the mode does
// not, and neither one silently resets the other.
func TestTheNotifyModeSurvivesASpecEditAndDoesNotReArm(t *testing.T) {
	tasks := NewSQLiteTaskStore(newTestDB(t))
	newTask(t, tasks, "price-check")

	always := types.NotifyAlways
	switched, err := tasks.UpdateTask("price-check", UpdateTaskInput{NotifyMode: &always})
	if err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}
	if switched.NotifyMode != types.NotifyAlways {
		t.Fatalf("notify_mode = %q, want always", switched.NotifyMode)
	}

	if err := tasks.MarkTaskNotified("price-check"); err != nil {
		t.Fatalf("MarkTaskNotified: %v", err)
	}

	// Changing only the mode leaves the armed state alone -- the condition it
	// describes has not changed.
	transition := types.NotifyOnTransition
	afterSwitchBack, err := tasks.UpdateTask("price-check", UpdateTaskInput{NotifyMode: &transition})
	if err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}
	if !afterSwitchBack.ConditionMet {
		t.Fatal("changing the mode must not re-arm the edge-trigger")
	}

	newSpec := sampleSpec()
	newSpec.Conditions[0].Value = "50"
	afterEdit, err := tasks.UpdateTask("price-check", UpdateTaskInput{Spec: newSpec})
	if err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}
	if afterEdit.NotifyMode != types.NotifyOnTransition {
		t.Fatalf("notify_mode = %q, a spec edit must not reset it", afterEdit.NotifyMode)
	}
}

func TestDeletingATaskCascadesItsRuns(t *testing.T) {
	db := newTestDB(t)
	tasks := NewSQLiteTaskStore(db)
	runs := NewSQLiteRunStore(db)

	newTask(t, tasks, "price-check")
	if _, err := runs.StartRun(StartRunInput{TaskID: "price-check", TriggerSource: types.TriggerCron}); err != nil {
		t.Fatalf("StartRun: %v", err)
	}

	deleted, err := tasks.DeleteTask("price-check")
	if err != nil || !deleted {
		t.Fatalf("DeleteTask = %t, %v", deleted, err)
	}

	listed, err := runs.ListRuns(ListRunsOptions{Limit: 10})
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if listed.Total != 0 {
		t.Fatalf("run history should cascade away, %d left", listed.Total)
	}
}

// A row whose stored JSON no longer parses reads back as a task with no spec,
// rather than taking down every request that lists tasks. It surfaces in the
// dashboard as orphaned and can be deleted from there.
func TestACorruptSpecDegradesToNil(t *testing.T) {
	db := newTestDB(t)
	tasks := NewSQLiteTaskStore(db)
	newTask(t, tasks, "price-check")

	if _, err := db.Exec(`UPDATE tasks SET spec = ? WHERE id = ?`, "{not json", "price-check"); err != nil {
		t.Fatalf("corrupting the spec: %v", err)
	}

	task, err := tasks.GetTask("price-check")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task == nil || task.Spec != nil {
		t.Fatalf("a corrupt spec should read back as nil, got %+v", task)
	}
}

func TestALegacyNullSpecDegradesToNil(t *testing.T) {
	db := newTestDB(t)
	tasks := NewSQLiteTaskStore(db)

	// A row written before tasks moved into the database: it has no spec and
	// cannot get one, but it still owns run history.
	if _, err := db.Exec(
		`INSERT INTO tasks (id, name, cron_expr) VALUES (?, ?, ?)`,
		"legacy", "Legacy", "* * * * *",
	); err != nil {
		t.Fatalf("inserting a legacy row: %v", err)
	}

	task, err := tasks.GetTask("legacy")
	if err != nil || task == nil {
		t.Fatalf("GetTask = %+v, %v", task, err)
	}
	if task.Spec != nil {
		t.Fatal("a null spec should read back as nil")
	}
}

func TestGetTaskReturnsNilForAnUnknownID(t *testing.T) {
	tasks := NewSQLiteTaskStore(newTestDB(t))

	task, err := tasks.GetTask("nope")
	if err != nil {
		t.Fatalf("an unknown id is not an error: %v", err)
	}
	if task != nil {
		t.Fatalf("expected nil, got %+v", task)
	}
}

// --- runs -------------------------------------------------------------------

func TestSweepInterruptedRunsResolvesDanglingRows(t *testing.T) {
	db := newTestDB(t)
	tasks := NewSQLiteTaskStore(db)
	runs := NewSQLiteRunStore(db)

	newTask(t, tasks, "price-check")
	// Written before execution, so a crash mid-run leaves it here forever.
	runID, err := runs.StartRun(StartRunInput{TaskID: "price-check", TriggerSource: types.TriggerCron})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}

	swept, err := runs.SweepInterruptedRuns()
	if err != nil || swept != 1 {
		t.Fatalf("SweepInterruptedRuns = %d, %v", swept, err)
	}

	run, err := runs.GetRun(runID)
	if err != nil || run == nil {
		t.Fatalf("GetRun = %+v, %v", run, err)
	}
	if run.Status != types.RunStatusFailed || run.FinishedAt == nil {
		t.Fatalf("an interrupted run should be failed and finished: %+v", run)
	}
}

func TestPruneOldRunsRespectsTheCutoff(t *testing.T) {
	db := newTestDB(t)
	tasks := NewSQLiteTaskStore(db)
	runs := NewSQLiteRunStore(db)
	newTask(t, tasks, "price-check")

	old := time.Now().UTC().AddDate(0, 0, -40).Format(time.RFC3339Nano)
	if _, err := db.Exec(
		`INSERT INTO runs (task_id, started_at, status) VALUES (?, ?, 'success')`,
		"price-check", old,
	); err != nil {
		t.Fatalf("inserting an old run: %v", err)
	}
	if _, err := runs.StartRun(StartRunInput{TaskID: "price-check", TriggerSource: types.TriggerCron}); err != nil {
		t.Fatalf("StartRun: %v", err)
	}

	pruned, err := runs.PruneOldRuns(30)
	if err != nil || pruned != 1 {
		t.Fatalf("PruneOldRuns = %d, %v -- only the run past the cutoff should go", pruned, err)
	}

	listed, err := runs.ListRuns(ListRunsOptions{Limit: 10})
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if listed.Total != 1 {
		t.Fatalf("recent runs should survive, %d left", listed.Total)
	}
}

func TestCompleteRunConvertsBooleansAndStoresTheResult(t *testing.T) {
	db := newTestDB(t)
	tasks := NewSQLiteTaskStore(db)
	runs := NewSQLiteRunStore(db)
	newTask(t, tasks, "price-check")

	runID, _ := runs.StartRun(StartRunInput{TaskID: "price-check", TriggerSource: types.TriggerManual})

	if err := runs.CompleteRun(CompleteRunInput{
		ID:           runID,
		Status:       types.RunStatusSuccess,
		ConditionMet: true,
		Notified:     true,
		HasResult:    true,
		Result:       &types.TaskResult{Value: 42.0, Raw: "42", URL: "https://example.com"},
	}); err != nil {
		t.Fatalf("CompleteRun: %v", err)
	}

	run, err := runs.GetRun(runID)
	if err != nil || run == nil {
		t.Fatalf("GetRun = %+v, %v", run, err)
	}

	// SQLite stores 0/1; the store converts at the boundary so types.Run can
	// honestly declare real booleans.
	if !run.ConditionMet || !run.Notified {
		t.Fatalf("booleans did not convert: %+v", run)
	}
	if run.TriggerSource != types.TriggerManual {
		t.Fatalf("trigger_source = %q", run.TriggerSource)
	}
	if run.ResultSummary == nil {
		t.Fatal("the result should be stored")
	}
	if run.TaskName == nil || *run.TaskName != "Task price-check" {
		t.Fatalf("task_name should be joined in, got %v", run.TaskName)
	}
}

// A result json.Marshal chokes on records a diagnostic rather than throwing the
// otherwise-good run away.
func TestAnUnserializableResultRecordsADiagnostic(t *testing.T) {
	db := newTestDB(t)
	tasks := NewSQLiteTaskStore(db)
	runs := NewSQLiteRunStore(db)
	newTask(t, tasks, "price-check")

	runID, _ := runs.StartRun(StartRunInput{TaskID: "price-check", TriggerSource: types.TriggerCron})

	if err := runs.CompleteRun(CompleteRunInput{
		ID: runID, Status: types.RunStatusSuccess, HasResult: true, Result: make(chan int),
	}); err != nil {
		t.Fatalf("CompleteRun: %v", err)
	}

	run, _ := runs.GetRun(runID)
	if run.ResultSummary == nil || *run.ResultSummary == "" {
		t.Fatal("an unserializable result should still record something")
	}
	if run.Status != types.RunStatusSuccess {
		t.Fatalf("the run should still be a success, got %q", run.Status)
	}
}

func TestListRunsComposesFiltersAndPaginates(t *testing.T) {
	db := newTestDB(t)
	tasks := NewSQLiteTaskStore(db)
	runs := NewSQLiteRunStore(db)
	newTask(t, tasks, "a")
	newTask(t, tasks, "b")

	for i := 0; i < 3; i++ {
		id, _ := runs.StartRun(StartRunInput{TaskID: "a", TriggerSource: types.TriggerCron})
		_ = runs.CompleteRun(CompleteRunInput{ID: id, Status: types.RunStatusSuccess})
	}
	failed, _ := runs.StartRun(StartRunInput{TaskID: "a", TriggerSource: types.TriggerCron})
	_ = runs.CompleteRun(CompleteRunInput{ID: failed, Status: types.RunStatusFailed, Error: "boom"})
	id, _ := runs.StartRun(StartRunInput{TaskID: "b", TriggerSource: types.TriggerCron})
	_ = runs.CompleteRun(CompleteRunInput{ID: id, Status: types.RunStatusSuccess})

	t.Run("filters compose and total reflects them", func(t *testing.T) {
		listed, err := runs.ListRuns(ListRunsOptions{
			TaskID: "a", Status: types.RunStatusSuccess, Limit: 10,
		})
		if err != nil {
			t.Fatalf("ListRuns: %v", err)
		}
		if listed.Total != 3 || len(listed.Runs) != 3 {
			t.Fatalf("total=%d len=%d, want 3 and 3", listed.Total, len(listed.Runs))
		}
	})

	t.Run("pages are disjoint", func(t *testing.T) {
		first, _ := runs.ListRuns(ListRunsOptions{Limit: 2, Offset: 0})
		second, _ := runs.ListRuns(ListRunsOptions{Limit: 2, Offset: 2})

		if first.Total != 5 || second.Total != 5 {
			t.Fatalf("total should ignore paging: %d, %d", first.Total, second.Total)
		}
		for _, a := range first.Runs {
			for _, b := range second.Runs {
				if a.ID == b.ID {
					t.Fatalf("run %d appeared on both pages", a.ID)
				}
			}
		}
	})
}

func TestGetLatestRunByTask(t *testing.T) {
	db := newTestDB(t)
	tasks := NewSQLiteTaskStore(db)
	runs := NewSQLiteRunStore(db)
	newTask(t, tasks, "a")
	newTask(t, tasks, "b")

	_, _ = runs.StartRun(StartRunInput{TaskID: "a", TriggerSource: types.TriggerCron})
	newestA, _ := runs.StartRun(StartRunInput{TaskID: "a", TriggerSource: types.TriggerCron})
	onlyB, _ := runs.StartRun(StartRunInput{TaskID: "b", TriggerSource: types.TriggerCron})

	latest, err := runs.GetLatestRunByTask()
	if err != nil {
		t.Fatalf("GetLatestRunByTask: %v", err)
	}

	if latest["a"] == nil || latest["a"].ID != newestA {
		t.Fatalf("latest for a = %+v, want run %d", latest["a"], newestA)
	}
	if latest["b"] == nil || latest["b"].ID != onlyB {
		t.Fatalf("latest for b = %+v, want run %d", latest["b"], onlyB)
	}
}

// The `changed` operator compares against the last *successful* run: an error
// says nothing about what the page holds, so comparing against one would report
// a change that never happened.
func TestGetLastSuccessfulResultSkipsFailures(t *testing.T) {
	db := newTestDB(t)
	tasks := NewSQLiteTaskStore(db)
	runs := NewSQLiteRunStore(db)
	newTask(t, tasks, "price-check")

	if got := runs.GetLastSuccessfulResult("price-check"); got != nil {
		t.Fatalf("a task with no runs should have no previous value, got %#v", got)
	}

	good, _ := runs.StartRun(StartRunInput{TaskID: "price-check", TriggerSource: types.TriggerCron})
	_ = runs.CompleteRun(CompleteRunInput{
		ID: good, Status: types.RunStatusSuccess, HasResult: true,
		Result: &types.TaskResult{Value: 99.0, Raw: "99"},
	})

	bad, _ := runs.StartRun(StartRunInput{TaskID: "price-check", TriggerSource: types.TriggerCron})
	_ = runs.CompleteRun(CompleteRunInput{ID: bad, Status: types.RunStatusFailed, Error: "boom"})

	previous, ok := runs.GetLastSuccessfulResult("price-check").(map[string]any)
	if !ok {
		t.Fatalf("expected a decoded result, got %#v", runs.GetLastSuccessfulResult("price-check"))
	}
	if previous["value"] != 99.0 {
		t.Fatalf("previous value = %#v, want 99 -- the failure must be skipped", previous["value"])
	}
}
