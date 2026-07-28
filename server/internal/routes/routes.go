package routes

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"breckr-server/internal/app"
	"breckr-server/internal/config"
	"breckr-server/internal/utils"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
)

func RegisterRoutes(application *app.Application, cfg *config.Config) *chi.Mux {
	r := chi.NewMux()

	r.Use(application.LoggingMiddleware.LogRequest)

	// In development the dashboard runs on Vite and proxies /api, so both modes
	// are same-origin; this is here for a client served from somewhere else.
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   cfg.Client.AllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PATCH", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	r.Route("/api", func(r chi.Router) {
		r.Get("/health", application.HealthHandler.HandleHealthCheck)

		r.Get("/tasks", application.TaskHandler.HandleGetAllTasks)
		r.Post("/tasks", application.TaskHandler.HandleCreateTask)
		r.Post("/tasks/test", application.TaskHandler.HandleTestTask)
		r.Patch("/tasks/{id}", application.TaskHandler.HandleUpdateTask)
		r.Delete("/tasks/{id}", application.TaskHandler.HandleDeleteTask)
		r.Post("/tasks/{id}/run-now", application.TaskHandler.HandleRunTaskNow)

		r.Get("/runs", application.RunHandler.HandleGetAllRuns)
		r.Get("/runs/{id}", application.RunHandler.HandleGetRun)

		r.Post("/notifications/test", application.NotificationHandler.HandleTestNotification)
	})

	registerDashboard(r, application, cfg)

	return r
}

// registerDashboard serves the built client from the same origin and port, so
// there is no CORS to configure and a reverse proxy is genuinely optional.
func registerDashboard(r *chi.Mux, application *app.Application, cfg *config.Config) {
	index := filepath.Join(cfg.Server.ClientDist, "index.html")

	if _, err := os.Stat(index); err != nil {
		application.Logger.Printf(
			"WARN: dashboard build not found at %s -- serving the API only (run `make build-client`)",
			cfg.Server.ClientDist)

		// Without the SPA there is still a fallback to install, so an unknown
		// path answers as JSON rather than chi's plain-text 404.
		r.NotFound(func(w http.ResponseWriter, req *http.Request) {
			utils.WriteError(w, http.StatusNotFound, "Not found.", "")
		})
		return
	}

	files := http.FileServer(http.Dir(cfg.Server.ClientDist))

	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		// An unmatched /api path is a client error, never the SPA shell --
		// returning index.html there would hand a fetch() a page of HTML and
		// surface as a JSON parse error miles from the cause.
		if strings.HasPrefix(req.URL.Path, "/api/") {
			utils.WriteError(w, http.StatusNotFound, "Not found.", "")
			return
		}

		// A real file wins; anything else is a client-side route and gets the
		// SPA shell.
		if path := filepath.Join(cfg.Server.ClientDist, filepath.Clean(req.URL.Path)); isFile(path) {
			files.ServeHTTP(w, req)
			return
		}

		http.ServeFile(w, req, index)
	})
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
