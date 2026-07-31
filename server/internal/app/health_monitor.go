package app

import (
	"context"
	"time"

	"breckr-server/internal/events"
	"breckr-server/internal/types"
)

// watchBrowserHealth publishes whenever the browser's reachability changes.
//
// Reachability lives in another process, so it is the one piece of dashboard
// state nothing can push -- somebody has to ask. Asking here means the answer is
// probed on a fixed schedule no matter how many dashboards are open, where
// before every open tab probed it every ten seconds. Each probe takes the global
// browser mutex, the same one every task run queues behind, so consolidating it
// cuts contention with real work and not just HTTP traffic.
//
// Only a change is published. A browser that stays up says nothing, which is the
// whole point of not polling.
func (a *Application) watchBrowserHealth(ctx context.Context) {
	ticker := time.NewTicker(types.HealthProbeInterval)
	defer ticker.Stop()

	// The dashboard fetches health when it connects, so the first probe is a
	// baseline to compare against rather than something to announce.
	previous := a.browser.CheckReachable(types.BrowserProbeTimeout)

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			current := a.browser.CheckReachable(types.BrowserProbeTimeout)
			if current == previous {
				continue
			}

			a.Logger.Printf("INFO: browser health changed (reachable=%t endpoint=%s)",
				current.Reachable, current.Endpoint)

			previous = current
			a.Events.Publish(events.ResourceHealth)
		}
	}
}
