import cron from "node-cron";
import type { ScheduledTask } from "node-cron";
import type { Task, TaskResult } from "@breckr/shared";
import { config } from "../config/index.ts";
import * as tasks from "../repositories/tasks.repository.ts";
import { compile } from "./executor.service.ts";
import type { Logger, ResolvedTask } from "../types/index.ts";

/**
 * The live cron registry.
 *
 * Tasks are stored in SQLite and authored from the dashboard, so this has to be
 * mutable at run time: a task added at 10:05 must start firing without a
 * restart. node-cron cannot change a schedule in place, so `reschedule` is
 * destroy-then-schedule.
 */

interface RegistryEntry {
  definition: ResolvedTask<TaskResult>;
  handle: ScheduledTask;
}

const registry = new Map<string, RegistryEntry>();

let triggerHandler: TriggerHandler | null = null;

export type TriggerHandler = (
  definition: ResolvedTask<TaskResult>,
  triggerSource: "cron"
) => void;

/**
 * Arm one task. Returns false for a row with no usable spec — legacy, or
 * corrupt JSON — which keeps its history but can never run.
 *
 * `noOverlap` stops a task stacking on itself when a run outlives its interval.
 * Cross-task collisions are handled separately by the browser mutex, because
 * the CDP server accepts only one connection at a time.
 */
export function register(task: Task, logger: Logger = console): boolean {
  const onTrigger = triggerHandler;
  if (!onTrigger) {
    throw new Error("register() called before scheduleAll() installed a handler.");
  }

  const { spec } = task;
  if (spec === null) {
    logger.error(
      { taskId: task.id },
      "Task has no usable spec and will not be scheduled"
    );
    return false;
  }

  unregister(task.id);

  const definition = compile({
    id: task.id,
    name: task.name,
    cron_expr: task.cron_expr,
    spec,
  });

  const handle = cron.schedule(
    definition.cron,
    () => {
      onTrigger(definition, "cron");
    },
    { name: definition.id, timezone: config.timezone, noOverlap: true }
  );

  // cron.schedule() arms immediately, so a disabled task has to be stopped
  // right back down.
  if (!task.enabled) handle.stop();

  registry.set(task.id, { definition, handle });
  return true;
}

/** Tear a task's schedule down. Safe to call for an id that was never armed. */
export function unregister(id: string): void {
  const entry = registry.get(id);
  if (!entry) return;

  entry.handle.destroy();
  registry.delete(id);
}

/**
 * Re-arm a task after its cron expression or spec changed.
 *
 * node-cron exposes no way to swap an expression on a live handle, so the old
 * one is destroyed and a new one scheduled in its place.
 */
export function reschedule(task: Task, logger: Logger = console): boolean {
  return register(task, logger);
}

/**
 * Install the trigger handler and arm everything currently stored.
 *
 * Unlike the file-based registry this replaced, a bad task does **not** fail the
 * boot. A spec is validated before it is written, so an unusable one means a
 * corrupt row — and refusing to start would lock the user out of the only UI
 * that could repair it. The row is logged, left unscheduled, and reported to the
 * dashboard as orphaned.
 */
export function scheduleAll(onTrigger: TriggerHandler, logger: Logger = console): void {
  triggerHandler = onTrigger;

  const stored = tasks.listTasks();
  let skipped = 0;

  for (const task of stored) {
    if (!register(task, logger)) skipped += 1;
  }

  const scheduled = [...registry.values()].filter(
    (entry) => entry.handle.getStatus() !== "stopped"
  ).length;

  logger.info(
    { scheduled, total: registry.size, skipped },
    "Registered cron schedules"
  );
}

export function getDefinition(id: string): ResolvedTask<TaskResult> | null {
  return registry.get(id)?.definition ?? null;
}

/** ISO-8601 of the next fire, or null while stopped. */
export function getNextRun(id: string): string | null {
  const entry = registry.get(id);
  if (!entry) return null;
  const next = entry.handle.getNextRun();
  return next ? next.toISOString() : null;
}

export function setEnabled(id: string, enabled: boolean): boolean {
  const entry = registry.get(id);
  if (!entry) return false;

  if (enabled) entry.handle.start();
  else entry.handle.stop();

  tasks.setTaskEnabled(id, enabled);
  return true;
}

export function listIds(): string[] {
  return [...registry.keys()];
}

export function destroyAll(): void {
  for (const { handle } of registry.values()) {
    handle.destroy();
  }
  registry.clear();
}
