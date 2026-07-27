import type { FastifyInstance } from "fastify";
import type { HealthResponse } from "@breckr/shared";
import { config } from "../config/index.ts";
import { checkBrowserReachable } from "../services/browser.service.ts";
import * as registry from "../services/registry.service.ts";

export function registerHealthRoutes(app: FastifyInstance): void {
  app.get("/api/health", async (): Promise<HealthResponse> => {
    const browser = await checkBrowserReachable();

    return {
      ok: true,
      // The browser being down is reported, not fatal: browserless tasks and
      // the dashboard keep working, and the run history stays readable.
      browser: { endpoint: config.browserWsEndpoint, ...browser },
      tasks: registry.listIds().length,
      timezone: config.timezone,
    };
  });
}
