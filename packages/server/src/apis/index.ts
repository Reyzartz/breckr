import fs from "node:fs";
import type { FastifyInstance } from "fastify";
import fastifyStatic from "@fastify/static";
import { config } from "../config/index.ts";
import { registerHealthRoutes } from "./health.routes.ts";
import { registerTaskRoutes } from "./tasks.routes.ts";
import { registerRunRoutes } from "./runs.routes.ts";

export async function registerRoutes(app: FastifyInstance): Promise<void> {
  registerHealthRoutes(app);
  registerTaskRoutes(app);
  registerRunRoutes(app);

  // In production the built dashboard is served from the same origin and port,
  // so there is no CORS to configure and nginx is genuinely optional.
  if (!config.isProduction) return;

  if (!fs.existsSync(config.dashboardDist)) {
    app.log.warn(
      { dist: config.dashboardDist },
      "NODE_ENV=production but the dashboard build is missing — run `npm run build`"
    );
    return;
  }

  await app.register(fastifyStatic, { root: config.dashboardDist });

  // SPA fallback: anything not under /api and not a real file is the app.
  app.setNotFoundHandler((request, reply) => {
    if (request.url.startsWith("/api/")) {
      return reply.code(404).send({ error: "Not found." });
    }
    return reply.sendFile("index.html");
  });
}
