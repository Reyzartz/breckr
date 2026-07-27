import type { Task, TaskSpec } from "@breckr/shared";
import { db, now, toBoolean, fromBoolean } from "./database.ts";
import { safeStringify } from "../utils/json.ts";

/** Raw shape as stored, before booleans are normalized and the spec parsed. */
interface TaskRow {
  id: string;
  name: string;
  cron_expr: string;
  enabled: number;
  condition_met: number;
  last_notified_at: string | null;
  spec: string | null;
  created_at: string | null;
  updated_at: string | null;
}

export interface CreateTaskInput {
  id: string;
  name: string;
  cron_expr: string;
  spec: TaskSpec;
  enabled?: boolean;
}

/** Only the fields present are written; the rest keep their stored values. */
export interface UpdateTaskInput {
  name?: string;
  cron_expr?: string;
  spec?: TaskSpec;
}

const insertStatement = db.prepare<{
  id: string;
  name: string;
  cron_expr: string;
  spec: string;
  enabled: number;
  at: string;
}>(`
  INSERT INTO tasks (id, name, cron_expr, spec, enabled, created_at, updated_at)
  VALUES (@id, @name, @cron_expr, @spec, @enabled, @at, @at)
`);

const selectOne = db.prepare<[string]>(`SELECT * FROM tasks WHERE id = ?`);
const selectAll = db.prepare(`SELECT * FROM tasks ORDER BY name`);

const deleteStatement = db.prepare<[string]>(`DELETE FROM tasks WHERE id = ?`);

const updateEnabled = db.prepare<{ id: string; enabled: number }>(
  `UPDATE tasks SET enabled = @enabled WHERE id = @id`
);

const updateConditionMet = db.prepare<{ id: string; condition_met: number }>(
  `UPDATE tasks SET condition_met = @condition_met WHERE id = @id`
);

const markNotified = db.prepare<{ id: string; at: string }>(
  `UPDATE tasks SET condition_met = 1, last_notified_at = @at WHERE id = @id`
);

/**
 * A stored spec that no longer parses reads back as null rather than throwing.
 *
 * The task then shows up as orphaned and can be deleted from the dashboard —
 * which beats a corrupt row taking down every request that lists tasks.
 */
function parseSpec(raw: string | null): TaskSpec | null {
  if (!raw) return null;
  try {
    const parsed: unknown = JSON.parse(raw);
    return typeof parsed === "object" && parsed !== null ? (parsed as TaskSpec) : null;
  } catch {
    return null;
  }
}

function toTask(row: TaskRow): Task {
  return {
    id: row.id,
    name: row.name,
    cron_expr: row.cron_expr,
    enabled: toBoolean(row.enabled),
    spec: parseSpec(row.spec),
    condition_met: toBoolean(row.condition_met),
    last_notified_at: row.last_notified_at,
  };
}

export function createTask(input: CreateTaskInput): Task {
  insertStatement.run({
    id: input.id,
    name: input.name,
    cron_expr: input.cron_expr,
    spec: safeStringify(input.spec),
    enabled: fromBoolean(input.enabled ?? true),
    at: now(),
  });

  // Non-null: the insert above either succeeded or threw.
  return getTask(input.id) as Task;
}

/**
 * Patch a task, returning it as stored afterwards.
 *
 * Editing the spec deliberately re-arms the edge-trigger: the persisted
 * `condition_met` describes the *old* condition, and carrying it over would let
 * a stale "already alerted" flag swallow the first alert of the new one.
 */
export function updateTask(id: string, patch: UpdateTaskInput): Task | null {
  const assignments: string[] = [];
  const params: Record<string, string | number> = { id, at: now() };

  if (patch.name !== undefined) {
    assignments.push("name = @name");
    params["name"] = patch.name;
  }
  if (patch.cron_expr !== undefined) {
    assignments.push("cron_expr = @cron_expr");
    params["cron_expr"] = patch.cron_expr;
  }
  if (patch.spec !== undefined) {
    assignments.push("spec = @spec", "condition_met = 0");
    params["spec"] = safeStringify(patch.spec);
  }

  if (assignments.length > 0) {
    assignments.push("updated_at = @at");
    db.prepare(`UPDATE tasks SET ${assignments.join(", ")} WHERE id = @id`).run(params);
  }

  return getTask(id);
}

/** Run history goes with it, through the ON DELETE CASCADE on runs.task_id. */
export function deleteTask(id: string): boolean {
  return deleteStatement.run(id).changes > 0;
}

export function getTask(id: string): Task | null {
  const row = selectOne.get(id) as TaskRow | undefined;
  return row ? toTask(row) : null;
}

export function listTasks(): Task[] {
  return (selectAll.all() as TaskRow[]).map(toTask);
}

export function setTaskEnabled(id: string, enabled: boolean): void {
  updateEnabled.run({ id, enabled: fromBoolean(enabled) });
}

/** Advance or re-arm the edge-trigger state. */
export function setTaskConditionMet(id: string, met: boolean): void {
  updateConditionMet.run({ id, condition_met: fromBoolean(met) });
}

/** Arm the trigger and stamp the delivery time, after a successful send. */
export function markTaskNotified(id: string): void {
  markNotified.run({ id, at: now() });
}
