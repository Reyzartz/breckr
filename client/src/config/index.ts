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
  /** How often the dashboard re-polls tasks, runs, health, and channels. */
  pollIntervalMs: 10_000,
} as const;

/**
 * Every query in this dashboard watches live server state -- a cron tick, a
 * run landing, a condition flipping -- so `staleTime: 0` is deliberate: there
 * is no "still fresh, skip the request" window the way there would be for
 * mostly-static data. Freshness instead comes from each query's own
 * `refetchInterval`, and the default `refetchOnWindowFocus` is left on, which
 * is a genuine improvement over the polling loop this replaces -- switching
 * back to the tab no longer waits out the rest of the interval.
 */
export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 0,
      retry: 1,
    },
  },
});
