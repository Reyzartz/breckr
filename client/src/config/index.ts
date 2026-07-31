/**
 * Runtime configuration.
 *
 * The API is same-origin in both modes — Vite proxies /api in development, and
 * the Go server serves this build itself in production — so the base path is a
 * relative prefix rather than an absolute URL, and there is no CORS to think
 * about either way.
 */
export const config = {
  apiBasePath: "/api",
  /**
   * The live connection. The dashboard does not poll: the server pushes a
   * "these resources changed" signal here and the client refetches only what it
   * names.
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
