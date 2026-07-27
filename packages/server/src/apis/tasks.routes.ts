import type { FastifyInstance, FastifyReply } from "fastify";
import type {
  CreateTaskRequest,
  TasksResponse,
  TestTaskRequest,
  TestTaskResponse,
  UpdateTaskRequest,
  TaskWithStatus,
} from "@breckr/shared";
import * as taskRepo from "../repositories/tasks.repository.ts";
import * as runRepo from "../repositories/runs.repository.ts";
import * as registry from "../services/registry.service.ts";
import { runTask } from "../services/runner.service.ts";
import { withPage } from "../services/browser.service.ts";
import { testSpec } from "../services/executor.service.ts";
import {
  SpecValidationError,
  resolveCron,
  validateName,
  validateSpec,
  validateTaskInput,
} from "../services/spec.service.ts";
import { fromCron } from "../services/schedule.service.ts";
import { config } from "../config/index.ts";
import { errorMessage } from "../utils/json.ts";

/**
 * A spec that failed validation is the caller's fault, not the server's, so it
 * comes back as a 400 naming the offending field — the dashboard renders that
 * message against the control the user got wrong.
 */
function replyValidationError(reply: FastifyReply, err: unknown): FastifyReply | never {
  if (err instanceof SpecValidationError) {
    return reply.code(400).send({ error: err.message, field: err.field });
  }
  throw err;
}

export function registerTaskRoutes(app: FastifyInstance): void {
  app.get("/api/tasks", async (): Promise<TasksResponse> => {
    const latestRuns = runRepo.getLatestRunByTask();

    const tasks: TaskWithStatus[] = taskRepo.listTasks().map((task) => ({
      ...task,
      // Derived rather than stored, so a row whose expression was written by
      // hand still opens in the form's builder — as `custom`.
      schedule: fromCron(task.cron_expr),
      last_run: latestRuns.get(task.id) ?? null,
      next_run: registry.getNextRun(task.id),
      // A row can carry no usable spec — written by the old file-based registry,
      // or corrupt JSON. The dashboard needs to know it can no longer be run.
      orphaned: registry.getDefinition(task.id) === null,
    }));

    return { tasks };
  });

  app.post<{ Body: CreateTaskRequest }>("/api/tasks", async (request, reply) => {
    let input;
    try {
      input = validateTaskInput(request.body);
    } catch (err) {
      return replyValidationError(reply, err);
    }

    if (taskRepo.getTask(input.id)) {
      return reply.code(409).send({
        error: `Task "${input.id}" already exists.`,
        field: "id",
      });
    }

    const created = taskRepo.createTask({
      ...input,
      enabled: request.body.enabled ?? true,
    });

    // Arm it now rather than at the next boot — a task you just saved is
    // expected to start running, not to wait for a restart.
    const scheduled = registry.register(created, request.log);

    const response: TaskWithStatus = {
      ...created,
      schedule: fromCron(created.cron_expr),
      last_run: null,
      next_run: registry.getNextRun(created.id),
      orphaned: !scheduled,
    };
    return reply.code(201).send(response);
  });

  app.patch<{ Params: { id: string }; Body: UpdateTaskRequest }>(
    "/api/tasks/:id",
    async (request, reply) => {
      const { id } = request.params;
      const body = request.body ?? {};

      const existing = taskRepo.getTask(id);
      if (!existing) {
        return reply.code(404).send({ error: `Unknown task "${id}".` });
      }

      const patch: taskRepo.UpdateTaskInput = {};
      try {
        if (body.name !== undefined) patch.name = validateName(body.name);
        if (body.schedule !== undefined || body.cron_expr !== undefined) {
          patch.cron_expr = resolveCron(body.schedule, body.cron_expr);
        }
        if (body.spec !== undefined) patch.spec = validateSpec(body.spec);
      } catch (err) {
        return replyValidationError(reply, err);
      }

      const hasDefinitionChange = Object.keys(patch).length > 0;
      const wantsEnabled = typeof body.enabled === "boolean" ? body.enabled : null;

      if (hasDefinitionChange) {
        taskRepo.updateTask(id, patch);

        // Written to the row *before* rescheduling: register() reads `enabled`
        // off the stored task to decide whether to arm the fresh handle, so a
        // toggle applied afterwards would be undone by it.
        if (wantsEnabled !== null) taskRepo.setTaskEnabled(id, wantsEnabled);

        // node-cron cannot swap an expression on a live handle, so the schedule
        // is destroyed and rebuilt from the row we just wrote.
        const updated = taskRepo.getTask(id);
        if (updated) registry.reschedule(updated, request.log);
      } else if (wantsEnabled !== null) {
        // setEnabled owns both the handle and the row, so there is nothing to
        // write here first.
        if (!registry.setEnabled(id, wantsEnabled)) {
          return reply
            .code(409)
            .send({ error: `Task "${id}" has no usable spec and cannot be scheduled.` });
        }
      }

      return {
        id,
        enabled: wantsEnabled ?? existing.enabled,
        next_run: registry.getNextRun(id),
      };
    }
  );

  app.delete<{ Params: { id: string } }>("/api/tasks/:id", async (request, reply) => {
    const { id } = request.params;

    if (!taskRepo.getTask(id)) {
      return reply.code(404).send({ error: `Unknown task "${id}".` });
    }

    registry.unregister(id);
    // Run history goes with it, through the ON DELETE CASCADE on runs.task_id.
    taskRepo.deleteTask(id);

    return reply.code(204).send();
  });

  /**
   * Run a draft spec once without saving it.
   *
   * Writes no run row and sends no notification, so it can be pressed freely
   * while getting a selector right. It still queues behind the browser mutex
   * like any other run.
   */
  app.post<{ Body: TestTaskRequest }>("/api/tasks/test", async (request, reply) => {
    let spec;
    try {
      spec = validateSpec(request.body?.spec);
    } catch (err) {
      return replyValidationError(reply, err);
    }

    const name = request.body.name?.trim() ?? "Untitled task";

    try {
      const outcome = await withPage(config.defaultTimeoutMs, (page) =>
        testSpec(page, spec, name)
      );

      const response: TestTaskResponse = {
        ok: true,
        result: outcome.result,
        conditionMet: outcome.conditionMet,
        message: outcome.message,
      };
      return response;
    } catch (err) {
      // A failing draft is the expected case while iterating on a selector —
      // report it as a result, not as a 500.
      request.log.info({ err: errorMessage(err) }, "Task test run failed");
      const response: TestTaskResponse = { ok: false, error: errorMessage(err) };
      return response;
    }
  });

  app.post<{ Params: { id: string } }>(
    "/api/tasks/:id/run-now",
    async (request, reply) => {
      const { id } = request.params;
      const definition = registry.getDefinition(id);

      if (!definition) {
        return reply.code(404).send({ error: `Unknown task "${id}".` });
      }

      // Deliberately runs even when the task is disabled — "run now" is an
      // explicit manual override, and it's how you test a task before enabling
      // it. It still queues behind the mutex like any scheduled run.
      const outcome = await runTask(definition, "manual", request.log);
      return reply.code(202).send(outcome);
    }
  );
}
