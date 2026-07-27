import Fastify from "fastify";
import cron from "node-cron";
import type { ScheduledTask } from "node-cron";
import { config } from "./config/index.ts";
import { RETENTION_CRON, RETENTION_JOB_NAME } from "./constants/index.ts";
import { closeDatabase } from "./repositories/database.ts";
import * as runRepo from "./repositories/runs.repository.ts";
import * as registry from "./services/registry.service.ts";
import { runTask } from "./services/runner.service.ts";
import { registerRoutes } from "./apis/index.ts";
import { errorMessage } from "./utils/json.ts";

const app = Fastify({
  logger: {
    level: process.env["LOG_LEVEL"] ?? "info",
    ...(config.isProduction
      ? {}
      : { transport: { target: "pino-pretty", options: { colorize: true } } }),
  },
});

let retentionHandle: ScheduledTask | undefined;

async function start(): Promise<void> {
  // A run row is written before the task executes, so anything still marked
  // 'running' at boot died with the previous process.
  const swept = runRepo.sweepInterruptedRuns();
  if (swept > 0) {
    app.log.warn({ count: swept }, "Marked interrupted runs as failed");
  }

  const pruned = runRepo.pruneOldRuns();
  if (pruned > 0) {
    app.log.info(
      { count: pruned, days: config.retentionDays },
      "Pruned old runs"
    );
  }

  // Tasks live in SQLite and are authored from the dashboard, so this reads the
  // database rather than a directory, and a task it cannot use is skipped with
  // an error rather than failing the boot — see registry.scheduleAll.
  registry.scheduleAll((definition, triggerSource) => {
    // Fire-and-forget: node-cron doesn't await this, and runTask never rejects,
    // but guard anyway so nothing can become an unhandled rejection.
    void runTask(definition, triggerSource, app.log).catch((err: unknown) => {
      app.log.error(
        { err: errorMessage(err), taskId: definition.id },
        "Unexpected runner failure"
      );
    });
  }, app.log);

  retentionHandle = cron.schedule(
    RETENTION_CRON,
    () => {
      const count = runRepo.pruneOldRuns();
      if (count > 0) app.log.info({ count }, "Pruned old runs");
    },
    { name: RETENTION_JOB_NAME, timezone: config.timezone, noOverlap: true }
  );

  await registerRoutes(app);
  await app.listen({ port: config.port, host: config.host });

  app.log.info(
    {
      browser: config.browserWsEndpoint,
      telegram: config.telegram.enabled ? "configured" : "disabled",
      timezone: config.timezone,
      tasks: registry.listIds().length,
    },
    "Web task monitor ready"
  );
}

async function shutdown(signal: string): Promise<void> {
  app.log.info({ signal }, "Shutting down");
  try {
    retentionHandle?.destroy();
    registry.destroyAll();
    await app.close();
    closeDatabase();
  } catch (err) {
    app.log.error({ err: errorMessage(err) }, "Error during shutdown");
  }
  process.exit(0);
}

for (const signal of ["SIGINT", "SIGTERM"] as const) {
  process.on(signal, () => {
    void shutdown(signal);
  });
}

start().catch((err: unknown) => {
  app.log.error({ err: errorMessage(err) }, "Failed to start");
  process.exit(1);
});
