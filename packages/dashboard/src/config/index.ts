/**
 * Runtime configuration.
 *
 * The API is same-origin in both modes — Vite proxies /api in development, and
 * Fastify serves this build itself in production — so the base path is a
 * relative prefix rather than an absolute URL, and there is no CORS to think
 * about either way.
 */
export const config = {
  apiBasePath: "/api",
  /** How often the dashboard re-fetches tasks, runs, and health. */
  pollIntervalMs: 10_000,
} as const;
