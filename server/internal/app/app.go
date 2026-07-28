// Package app wires everything together: stores, services, handlers, and the
// lifecycle around them.
package app

import (
	"context"
	"database/sql"
	"log"
	"os"

	"breckr-server/internal/api"
	"breckr-server/internal/browser"
	"breckr-server/internal/config"
	"breckr-server/internal/executor"
	"breckr-server/internal/middleware"
	"breckr-server/internal/migrations"
	"breckr-server/internal/notifier"
	"breckr-server/internal/runner"
	"breckr-server/internal/scheduler"
	"breckr-server/internal/store"
	"breckr-server/internal/types"
)

type Application struct {
	Logger            *log.Logger
	Database          *sql.DB
	Registry          *scheduler.Registry
	Runner            *runner.Runner
	RunStore          store.RunStore
	HealthHandler     *api.HealthHandler
	TaskHandler       *api.TaskHandler
	RunHandler        *api.RunHandler
	LoggingMiddleware *middleware.LoggingMiddleware

	cfg *config.Config
}

func NewApplication(cfg *config.Config) (*Application, error) {
	db, err := store.Open(cfg)
	if err != nil {
		return nil, err
	}

	if err := store.MigrateFS(db, migrations.FS, "."); err != nil {
		return nil, err
	}

	logger := log.New(os.Stdout, "", log.LstdFlags)

	taskStore := store.NewSQLiteTaskStore(db)
	runStore := store.NewSQLiteRunStore(db)

	browserPool := browser.NewPool(cfg)
	telegram := notifier.NewTelegram(cfg.Telegram, logger)

	// The executor reaches run history only for the `changed` operator, through
	// a one-method interface -- which is what keeps its operator table testable
	// without a database.
	taskExecutor := executor.New(runStore, cfg.Browser.DefaultTimeout)

	taskRunner := runner.New(taskStore, runStore, browserPool, telegram, logger)
	registry := scheduler.New(cfg, taskStore, taskExecutor, logger)

	return &Application{
		Logger:        logger,
		Database:      db,
		Registry:      registry,
		Runner:        taskRunner,
		RunStore:      runStore,
		HealthHandler: api.NewHealthHandler(cfg, browserPool, registry),
		TaskHandler: api.NewTaskHandler(
			logger, taskStore, runStore, registry, taskRunner,
			browserPool, cfg.Browser.DefaultTimeout,
		),
		RunHandler:        api.NewRunHandler(logger, runStore),
		LoggingMiddleware: middleware.NewLoggingMiddleware(logger),
		cfg:               cfg,
	}, nil
}

// Boot resolves anything the last shutdown left behind and arms every schedule.
func (a *Application) Boot() error {
	// A run row is written before the task executes, so a crash mid-run leaves
	// a dangling 'running' row. Resolve those here -- otherwise they stay "in
	// progress" forever in the dashboard.
	if swept, err := a.RunStore.SweepInterruptedRuns(); err != nil {
		a.Logger.Printf("ERROR: could not sweep interrupted runs: %v", err)
	} else if swept > 0 {
		a.Logger.Printf("INFO: marked %d interrupted run(s) as failed", swept)
	}

	a.prune()

	if err := a.Registry.ScheduleAll(a.onTrigger); err != nil {
		return err
	}

	// The retention sweep rides the same scheduler as the tasks rather than
	// needing a second timer, so it inherits the configured timezone.
	if _, err := a.Registry.Cron().AddFunc(types.RetentionCron, a.prune); err != nil {
		return err
	}

	a.Registry.Start()
	return nil
}

// onTrigger is what a fired schedule calls. Runs are fire-and-forget: RunTask
// never returns an error, and the outcome lands on the run row.
func (a *Application) onTrigger(definition *types.ResolvedTask, source types.TriggerSource) {
	a.Runner.RunTask(context.Background(), definition, source)
}

func (a *Application) prune() {
	count, err := a.RunStore.PruneOldRuns(a.cfg.Runtime.RetentionDays)
	if err != nil {
		a.Logger.Printf("ERROR: could not prune old runs: %v", err)
		return
	}
	if count > 0 {
		a.Logger.Printf("INFO: pruned %d old run(s)", count)
	}
}

// Shutdown stops the scheduler, waiting for in-flight runs, then closes the
// database.
func (a *Application) Shutdown() {
	a.Registry.Stop()

	if err := a.Database.Close(); err != nil {
		a.Logger.Printf("ERROR: could not close the database: %v", err)
	}
}
