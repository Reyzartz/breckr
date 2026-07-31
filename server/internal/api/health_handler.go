package api

import (
	"net/http"
	"time"

	"breckr-server/internal/config"
	"breckr-server/internal/scheduler"
	"breckr-server/internal/store"
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
	channels store.ChannelStore
}

func NewHealthHandler(
	cfg *config.Config,
	browser BrowserProbe,
	registry *scheduler.Registry,
	channels store.ChannelStore,
) *HealthHandler {
	return &HealthHandler{cfg: cfg, browser: browser, registry: registry, channels: channels}
}

func (hh *HealthHandler) HandleHealthCheck(w http.ResponseWriter, r *http.Request) {
	// Counted rather than probed: a real probe would send a message to the
	// user's chat every time health was checked.
	//
	// A failed count reports zero, which reads as "not configured" -- the
	// dashboard then warns, which is the right way to be wrong here.
	enabled, err := hh.channels.CountEnabledChannels()
	if err != nil {
		enabled = 0
	}

	utils.WriteJSONResponse(w, http.StatusOK, utils.Envelope{
		"data": types.HealthResponse{
			OK: true,
			// The browser being down is reported, not fatal: the dashboard
			// keeps working and the run history stays readable.
			Browser: hh.browser.CheckReachable(types.BrowserProbeTimeout),
			Notifications: types.NotifierHealth{
				Configured: enabled > 0,
				Channels:   enabled,
			},
			Tasks:    len(hh.registry.ListIDs()),
			Timezone: hh.cfg.Runtime.Timezone,
		},
	})
}
