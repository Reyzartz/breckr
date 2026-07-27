import type { FastifyInstance } from "fastify";
import type { RunStatus } from "@breckr/shared";
import * as runRepo from "../repositories/runs.repository.ts";
import {
  RUN_STATUSES,
  DEFAULT_RUN_LIMIT,
  MAX_RUN_LIMIT,
} from "../constants/index.ts";

function clampLimit(raw: unknown): number {
  const n = Number(raw);
  if (!Number.isFinite(n) || n <= 0) return DEFAULT_RUN_LIMIT;
  // Cap so one request cannot pull the entire history.
  return Math.min(Math.floor(n), MAX_RUN_LIMIT);
}

function clampOffset(raw: unknown): number {
  const n = Number(raw);
  if (!Number.isFinite(n) || n < 0) return 0;
  return Math.floor(n);
}

function isRunStatus(value: string): value is RunStatus {
  return (RUN_STATUSES as readonly string[]).includes(value);
}

interface ListRunsQuery {
  task_id?: string;
  status?: string;
  limit?: string;
  offset?: string;
}

export function registerRunRoutes(app: FastifyInstance): void {
  app.get<{ Querystring: ListRunsQuery }>("/api/runs", async (request, reply) => {
    const { task_id: taskId, status } = request.query;

    // Assigning through a narrowed local is what carries the type guard past
    // the early return — a negated guard alone does not narrow the outer value.
    let statusFilter: RunStatus | undefined;
    if (status) {
      if (!isRunStatus(status)) {
        return reply
          .code(400)
          .send({ error: `status must be one of ${RUN_STATUSES.join(", ")}.` });
      }
      statusFilter = status;
    }

    return runRepo.listRuns({
      taskId: taskId || undefined,
      status: statusFilter,
      limit: clampLimit(request.query.limit),
      offset: clampOffset(request.query.offset),
    });
  });

  app.get<{ Params: { id: string } }>("/api/runs/:id", async (request, reply) => {
    const run = runRepo.getRun(Number(request.params.id));
    if (!run) {
      return reply.code(404).send({ error: "Run not found." });
    }
    return run;
  });
}
