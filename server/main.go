package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"breckr-server/internal/app"
	"breckr-server/internal/config"
	"breckr-server/internal/routes"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}

	application, err := app.NewApplication(cfg)
	if err != nil {
		panic(err)
	}
	defer application.Shutdown()

	if err := application.Boot(); err != nil {
		panic(err)
	}

	Start(application, cfg)
}

func Start(application *app.Application, cfg *config.Config) {
	r := routes.RegisterRoutes(application, cfg)
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)

	server := &http.Server{
		Addr:        addr,
		Handler:     r,
		IdleTimeout: time.Minute,
		ReadTimeout: 30 * time.Second,
		// No route waits on a run any more -- "run now" answers as soon as the
		// run is started and the outcome arrives over /api/events -- so this is
		// back to an ordinary bound rather than a full browser timeout.
		//
		// /api/events outlives it regardless: the websocket handler clears the
		// inherited deadlines once the connection is hijacked, and bounds each
		// frame on its own instead.
		WriteTimeout: 30 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		application.Logger.Printf("INFO: listening on %s (timezone %s)", addr, cfg.Runtime.Timezone)

		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			application.Logger.Printf("ERROR: %v", err)
			stop()
		}
	}()

	<-ctx.Done()
	application.Logger.Printf("INFO: shutting down")

	// Stop accepting new requests first, then let Shutdown (deferred in main)
	// drain the scheduler -- in that order, so an in-flight "run now" is not
	// racing a scheduler that has already gone away.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		application.Logger.Printf("ERROR: could not shut down cleanly: %v", err)
	}
}
