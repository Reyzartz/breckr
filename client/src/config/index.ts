import { QueryClient } from "@tanstack/react-query";

/**
 * Runtime configuration.
 *
 * The API is same-origin in both modes -- Vite proxies /api in development,
 * and the Go server serves this build itself in production -- so the base URL
 * is a relative prefix rather than an absolute one, and there is no CORS to
 * think about either way.
 */
export const config = {
  apiBaseUrl: "/api",
  /**
   * Above the Go server's own request budget (DEFAULT_TIMEOUT_MS plus a 30s
   * margin -- see server/main.go's http.Server.WriteTimeout), so a slow but
   * legitimate `/tasks/test` or `/tasks/:id/run-now` is never cut off by the
   * client first.
   */
  apiTimeoutMs: 65_000,
  /**
   * The live connection. The dashboard does not poll: the server pushes a
   * "these resources changed" signal here and the client invalidates only the
   * named queries.
   */
  eventsPath: "/events",
  /** First reconnect delay after the socket drops. */
  reconnectBaseMs: 1_000,
  /**
   * Ceiling on the backoff. Low enough that a server restart is picked up
   * without the user reaching for the reload button.
   */
  reconnectMaxMs: 15_000,
} as const;

/**
 * Every query in this dashboard watches live server state -- a cron tick, a
 * run landing, a condition flipping -- so `staleTime: 0` is deliberate: there
 * is no "still fresh, skip the request" window the way there would be for
 * mostly-static data.
 *
 * Freshness comes from the event socket invalidating queries as the server
 * reports changes, not from polling. `refetchOnWindowFocus` (the default) is
 * kept as a fallback net -- if the socket is mid-reconnect when the tab
 * regains focus, switching back still gets a fresh read rather than a stale
 * one held over from before.
 */
export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 0,
      retry: 1,
    },
  },
});
