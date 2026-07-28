// Package scheduler owns the live cron registry.
package scheduler

import (
	"log"
	"sync"
	"time"

	"breckr-server/internal/config"
	"breckr-server/internal/executor"
	"breckr-server/internal/store"
	"breckr-server/internal/types"

	"github.com/robfig/cron/v3"
)

// TriggerHandler is called when a task's schedule fires.
type TriggerHandler func(definition *types.ResolvedTask, triggerSource types.TriggerSource)

type entry struct {
	definition *types.ResolvedTask
	// Zero while the task is disabled: robfig/cron has no per-entry pause, so a
	// disabled task is removed from the cron and kept here. The entry has to
	// stay in the map -- "run now" deliberately works on a disabled task, and
	// that needs its compiled definition.
	entryID cron.EntryID
}

// Registry is the live cron registry.
//
// Tasks are stored in SQLite and authored from the dashboard, so this has to be
// mutable at run time: a task added at 10:05 must start firing without a
// restart. Neither node-cron nor robfig/cron can change a schedule in place, so
// Reschedule is destroy-then-schedule.
type Registry struct {
	cron     *cron.Cron
	tasks    store.TaskStore
	executor *executor.Executor
	logger   *log.Logger

	mu      sync.Mutex
	entries map[string]*entry
	onFire  TriggerHandler
}

func New(
	cfg *config.Config,
	tasks store.TaskStore,
	exec *executor.Executor,
	logger *log.Logger,
) *Registry {
	return &Registry{
		cron: cron.New(
			cron.WithLocation(cfg.Runtime.Location),
			cron.WithParser(executor.Parser),
			// Stops a task stacking on itself when a run outlives its interval.
			// Cross-task collisions are handled separately by the browser mutex,
			// because the CDP server accepts only one connection at a time.
			cron.WithChain(cron.SkipIfStillRunning(cron.PrintfLogger(logger))),
		),
		tasks:    tasks,
		executor: exec,
		logger:   logger,
		entries:  map[string]*entry{},
	}
}

// Start begins firing schedules.
func (r *Registry) Start() { r.cron.Start() }

// Stop halts the scheduler and waits for in-flight jobs to finish.
func (r *Registry) Stop() { <-r.cron.Stop().Done() }

// Cron exposes the underlying scheduler, so the retention sweep can be added
// alongside the tasks rather than needing a second timer.
func (r *Registry) Cron() *cron.Cron { return r.cron }

// ScheduleAll installs the trigger handler and arms everything currently stored.
//
// A bad task does *not* fail the boot. A spec is validated before it is written,
// so an unusable one means a corrupt row -- and refusing to start would lock the
// user out of the only UI that could repair it. The row is logged, left
// unscheduled, and reported to the dashboard as orphaned.
func (r *Registry) ScheduleAll(onFire TriggerHandler) error {
	r.mu.Lock()
	r.onFire = onFire
	r.mu.Unlock()

	stored, err := r.tasks.ListTasks()
	if err != nil {
		return err
	}

	skipped := 0
	scheduled := 0
	for _, task := range stored {
		if !r.Register(task) {
			skipped++
			continue
		}
		if task.Enabled {
			scheduled++
		}
	}

	r.logger.Printf("INFO: registered cron schedules (scheduled=%d total=%d skipped=%d)",
		scheduled, len(stored), skipped)
	return nil
}

// Register arms one task. Reports false for a row with no usable spec -- legacy,
// or corrupt JSON -- which keeps its history but can never run.
func (r *Registry) Register(task *types.Task) bool {
	if task.Spec == nil {
		r.logger.Printf("ERROR: task %q has no usable spec and will not be scheduled", task.ID)
		return false
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.onFire == nil {
		r.logger.Printf("ERROR: Register called for task %q before ScheduleAll installed a handler", task.ID)
		return false
	}

	r.removeLocked(task.ID)

	definition := r.executor.Compile(executor.CompilableTask{
		ID:       task.ID,
		Name:     task.Name,
		CronExpr: task.CronExpr,
		Spec:     task.Spec,
	})

	current := &entry{definition: definition}

	if task.Enabled {
		onFire := r.onFire
		entryID, err := r.cron.AddFunc(definition.Cron, func() {
			onFire(definition, types.TriggerCron)
		})
		if err != nil {
			r.logger.Printf("ERROR: task %q has an unschedulable cron expression %q: %v",
				task.ID, definition.Cron, err)
			return false
		}
		current.entryID = entryID
	}

	r.entries[task.ID] = current
	return true
}

// Unregister tears a task's schedule down. Safe for an id that was never armed.
func (r *Registry) Unregister(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.removeLocked(id)
}

func (r *Registry) removeLocked(id string) {
	current, ok := r.entries[id]
	if !ok {
		return
	}
	if current.entryID != 0 {
		r.cron.Remove(current.entryID)
	}
	delete(r.entries, id)
}

// Reschedule re-arms a task after its cron expression or spec changed.
//
// There is no way to swap an expression on a live entry, so the old one is
// removed and a new one added in its place.
func (r *Registry) Reschedule(task *types.Task) bool {
	return r.Register(task)
}

func (r *Registry) GetDefinition(id string) *types.ResolvedTask {
	r.mu.Lock()
	defer r.mu.Unlock()

	if current, ok := r.entries[id]; ok {
		return current.definition
	}
	return nil
}

// GetNextRun is the ISO-8601 time of the next fire, or nil while disabled.
//
// Computed from the entry's schedule rather than read off Entry.Next: the
// scheduler only fills Next in once its run loop has seen the entry, so reading
// the field would report null for every task between ScheduleAll and the first
// tick -- which includes the dashboard's first load after a restart.
func (r *Registry) GetNextRun(id string) *string {
	r.mu.Lock()
	defer r.mu.Unlock()

	current, ok := r.entries[id]
	if !ok || current.entryID == 0 {
		return nil
	}

	schedule := r.cron.Entry(current.entryID).Schedule
	if schedule == nil {
		return nil
	}

	next := schedule.Next(time.Now())
	if next.IsZero() {
		return nil
	}

	formatted := next.UTC().Format(time.RFC3339Nano)
	return &formatted
}

// SetEnabled arms or disarms a task, persisting the change. Reports false for a
// task that is not in the registry -- one with no usable spec.
func (r *Registry) SetEnabled(id string, enabled bool) bool {
	r.mu.Lock()

	current, ok := r.entries[id]
	if !ok {
		r.mu.Unlock()
		return false
	}

	switch {
	case enabled && current.entryID == 0:
		onFire := r.onFire
		definition := current.definition
		entryID, err := r.cron.AddFunc(definition.Cron, func() {
			onFire(definition, types.TriggerCron)
		})
		if err != nil {
			r.mu.Unlock()
			r.logger.Printf("ERROR: could not arm task %q: %v", id, err)
			return false
		}
		current.entryID = entryID

	case !enabled && current.entryID != 0:
		r.cron.Remove(current.entryID)
		current.entryID = 0
	}

	r.mu.Unlock()

	if err := r.tasks.SetTaskEnabled(id, enabled); err != nil {
		r.logger.Printf("ERROR: could not persist enabled=%t for task %q: %v", enabled, id, err)
	}
	return true
}

func (r *Registry) ListIDs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	ids := make([]string, 0, len(r.entries))
	for id := range r.entries {
		ids = append(ids, id)
	}
	return ids
}
