import { config } from "../../config/index.ts";
import type { ChangeEvent, MonitorResource } from "../../types/index.ts";

/**
 * Absolute ws(s):// URL for the change stream.
 *
 * Derived from the page rather than configured, so the socket follows the
 * dashboard wherever it is served from: Vite proxies it in development, the Go
 * server serves both itself in production, and https upgrades to wss.
 */
export function eventsUrl(): string {
  const url = new URL(`${config.apiBaseUrl}${config.eventsPath}`, location.href);
  url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
  return url.toString();
}

const RESOURCES: readonly string[] = [
  "tasks",
  "runs",
  "health",
  "channels",
] satisfies readonly MonitorResource[];

function isResource(value: unknown): value is MonitorResource {
  return typeof value === "string" && RESOURCES.includes(value);
}

/**
 * Parse one frame, or return null.
 *
 * Unknown event types and unknown resource names are dropped rather than
 * treated as errors: a newer server should be able to add either without
 * breaking a dashboard that is still open from before the deploy.
 */
export function parseChangeEvent(data: unknown): ChangeEvent | null {
  if (typeof data !== "string") return null;

  let body: unknown;
  try {
    body = JSON.parse(data);
  } catch {
    return null;
  }

  if (typeof body !== "object" || body === null) return null;

  const { type, resources } = body as Partial<ChangeEvent>;
  if (type !== "changed" || !Array.isArray(resources)) return null;

  const known = resources.filter(isResource);
  return known.length > 0 ? { type, resources: known } : null;
}
