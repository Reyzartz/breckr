// Package app wires everything together: stores, services, handlers, and the
// lifecycle around them.
package app

import (
	"context"
	"database/sql"
	"log"
	"net/netip"
	"os"
	"strings"

	"breckr-server/internal/api"
	"breckr-server/internal/auth"
	"breckr-server/internal/browser"
	"breckr-server/internal/config"
	"breckr-server/internal/crypto"
	"breckr-server/internal/events"
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
	Events            *events.Bus
	HealthHandler     *api.HealthHandler
	TaskHandler       *api.TaskHandler
	RunHandler        *api.RunHandler
	ChannelHandler    *api.ChannelHandler
	EventsHandler     *api.EventsHandler
	AuthHandler       *api.AuthHandler
	LoggingMiddleware *middleware.LoggingMiddleware
	AuthMiddleware    *middleware.AuthMiddleware

	cfg     *config.Config
	browser *browser.Pool
	// stopHealth ends the reachability watcher. Nil until Boot has run.
	stopHealth context.CancelFunc
}

func NewApplication(cfg *config.Config) (*Application, error) {
	db, err := store.Open(cfg)
	if err != nil {
		return nil, err
	}

	if err := store.MigrateFS(db, migrations.FS, "."); err != nil {
		return nil, err
	}

	// Before any store, because the channel store cannot be built without it.
	// Failing here rather than at first send means a missing or unreadable key
	// file is a boot error, not a missed alert.
	key, err := crypto.LoadOrCreateKey(cfg.Security.KeyFile)
	if err != nil {
		return nil, err
	}
	cipher, err := crypto.New(key)
	if err != nil {
		return nil, err
	}

	// The same master key, one derivation removed. Deriving rather than
	// generating a second secret means there is still exactly one file to back
	// up beside the database.
	sessions, err := auth.NewSessions(
		key, cfg.Security.AuthPassword, cfg.Security.SessionTTL, cfg.Security.CookieSecure,
	)
	if err != nil {
		return nil, err
	}

	logger := log.New(os.Stdout, "", log.LstdFlags)

	taskStore := store.NewSQLiteTaskStore(db)
	runStore := store.NewSQLiteRunStore(db)
	channelStore := store.NewSQLiteChannelStore(db, cipher)

	browserPool := browser.NewPool(cfg)
	dispatcher := notifier.NewDispatcher(channelStore, logger)

	// Every write path publishes here and every open dashboard subscribes, which
	// is what replaced the client's polling loop.
	bus := events.New()

	// The executor reaches run history only for the `changed` operator, through
	// a one-method interface -- which is what keeps its operator table testable
	// without a database.
	taskExecutor := executor.New(runStore, cfg.Browser.DefaultTimeout)

	taskRunner := runner.New(taskStore, runStore, channelStore, browserPool, dispatcher, bus, logger)
	registry := scheduler.New(cfg, taskStore, taskExecutor, logger)

	return &Application{
		Logger:        logger,
		Database:      db,
		Registry:      registry,
		Runner:        taskRunner,
		RunStore:      runStore,
		Events:        bus,
		HealthHandler: api.NewHealthHandler(cfg, browserPool, registry, channelStore),
		TaskHandler: api.NewTaskHandler(
			logger, taskStore, runStore, channelStore, registry, taskRunner,
			browserPool, bus, cfg.Browser.DefaultTimeout,
		),
		RunHandler: api.NewRunHandler(logger, runStore, channelStore),
		// The same dispatcher the runner holds, so a test send exercises the
		// delivery path a real alert takes rather than a parallel one.
		ChannelHandler:    api.NewChannelHandler(logger, channelStore, dispatcher, bus),
		EventsHandler:     api.NewEventsHandler(logger, bus, cfg.Client.AllowedOrigins),
		AuthHandler:       api.NewAuthHandler(logger, sessions, auth.NewThrottle()),
		LoggingMiddleware: middleware.NewLoggingMiddleware(logger),
		AuthMiddleware:    middleware.NewAuthMiddleware(sessions),
		cfg:               cfg,
		browser:           browserPool,
	}, nil
}

// Boot resolves anything the last shutdown left behind and arms every schedule.
func (a *Application) Boot() error {
	a.warnIfUnprotected()

	// A run row is written before the task executes, so a crash mid-run leaves
	// a dangling 'running' row. Resolve those here -- otherwise they stay "in
	// progress" forever in the dashboard.
	if swept, err := a.RunStore.SweepInterruptedRuns(); err != nil {
		a.Logger.Printf("ERROR: could not sweep interrupted runs: %v", err)
	} else if swept > 0 {
		a.Logger.Printf("INFO: marked %d interrupted run(s) as failed", swept)
		a.Events.Publish(events.ResourceRuns, events.ResourceTasks)
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

	healthCtx, stopHealth := context.WithCancel(context.Background())
	a.stopHealth = stopHealth
	go a.watchBrowserHealth(healthCtx)

	return nil
}

// warnIfUnprotected says so when the server is listening beyond loopback with no
// password.
//
// It cannot know whether that is actually dangerous: the image binds 0.0.0.0 so
// that `docker run -p` works at all, and compose then publishes only to
// 127.0.0.1 -- a perfectly safe arrangement that looks identical from in here.
// So it describes what is reachable and leaves the judgement to whoever can see
// the port mapping.
func (a *Application) warnIfUnprotected() {
	if a.cfg.Security.AuthPassword != "" || isLoopback(a.cfg.Server.Host) {
		return
	}

	a.Logger.Printf(
		"WARN: AUTH_PASSWORD is not set and the server is bound to %s -- anyone who can reach "+
			"this port can create tasks, browse run history and see your channels. Set AUTH_PASSWORD "+
			"unless this port is reachable only from your own machine.",
		a.cfg.Server.Host,
	)
}

func isLoopback(host string) bool {
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		return false
	case "localhost":
		return true
	}

	address, err := netip.ParseAddr(strings.Trim(host, "[]"))
	return err == nil && address.IsLoopback()
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
		a.Events.Publish(events.ResourceRuns, events.ResourceTasks)
	}
}

// Shutdown stops the scheduler, waiting for in-flight runs, then closes the
// database.
func (a *Application) Shutdown() {
	if a.stopHealth != nil {
		a.stopHealth()
	}

	a.Registry.Stop()

	if err := a.Database.Close(); err != nil {
		a.Logger.Printf("ERROR: could not close the database: %v", err)
	}
}
