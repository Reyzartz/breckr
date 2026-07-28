package api

import (
	"net/http"
	"time"

	"breckr-server/internal/config"
	"breckr-server/internal/scheduler"
	"breckr-server/internal/types"
	"breckr-server/internal/utils"
)

// BrowserProbe is the slice of the browser pool the health route needs.
type BrowserProbe interface {
	CheckReachable(timeout time.Duration) types.BrowserHealth
}

type HealthHandler struct {
	cfg      *config.Config
	browser  BrowserProbe
	registry *scheduler.Registry
}

func NewHealthHandler(
	cfg *config.Config,
	browser BrowserProbe,
	registry *scheduler.Registry,
) *HealthHandler {
	return &HealthHandler{cfg: cfg, browser: browser, registry: registry}
}

func (hh *HealthHandler) HandleHealthCheck(w http.ResponseWriter, r *http.Request) {
	utils.WriteJSONResponse(w, http.StatusOK, utils.Envelope{
		"data": types.HealthResponse{
			OK: true,
			// The browser being down is reported, not fatal: the dashboard
			// keeps working and the run history stays readable.
			Browser:  hh.browser.CheckReachable(types.BrowserProbeTimeout),
			Tasks:    len(hh.registry.ListIDs()),
			Timezone: hh.cfg.Runtime.Timezone,
		},
	})
}
