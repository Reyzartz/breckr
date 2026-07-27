import type { Run, RunStatus } from "@breckr/shared";
import { db, now, toBoolean, fromBoolean } from "./database.ts";
import { safeStringify } from "../utils/json.ts";
import { config } from "../config/index.ts";
import type { StartRunInput, CompleteRunInput } from "../types/index.ts";

/** Raw shape as stored, before booleans are normalized. */
interface RunRow {
  id: number;
  task_id: string;
  started_at: string;
  finished_at: string | null;
  status: RunStatus;
  condition_met: number;
  notified: number;
  trigger_source: Run["trigger_source"];
  result_summary: string | null;
  error: string | null;
  task_name?: string | null;
}

/**
 * SQLite stores booleans as 0/1. Converting here, at the boundary, is what lets
 * the shared `Run` type honestly declare `condition_met: boolean` — otherwise
 * every consumer has to remember to coerce.
 */
function toRun(row: RunRow): Run {
  return {
    ...row,
    condition_met: toBoolean(row.condition_met),
    notified: toBoolean(row.notified),
  };
}

const insertRun = db.prepare<{
  task_id: string;
  started_at: string;
  trigger_source: string;
}>(`
  INSERT INTO runs (task_id, started_at, status, trigger_source)
  VALUES (@task_id, @started_at, 'running', @trigger_source)
`);

const updateRun = db.prepare<{
  id: number;
  finished_at: string;
  status: string;
  condition_met: number;
  notified: number;
  result_summary: string | null;
  error: string | null;
}>(`
  UPDATE runs SET
    finished_at    = @finished_at,
    status         = @status,
    condition_met  = @condition_met,
    notified       = @notified,
    result_summary = @result_summary,
    error          = @error
  WHERE id = @id
`);

const selectRun = db.prepare<[number]>(`
  SELECT runs.*, tasks.name AS task_name
  FROM runs LEFT JOIN tasks ON tasks.id = runs.task_id
  WHERE runs.id = ?
`);

/** Most recent run per task, for the "last run" column on the task list. */
const selectLatestPerTask = db.prepare(`
  SELECT r.* FROM runs r
  JOIN (
    SELECT task_id, MAX(id) AS id FROM runs GROUP BY task_id
  ) latest ON latest.id = r.id
`);

/** Backs the `changed` operator: what this task last saw when it worked. */
const selectLastSuccessfulResult = db.prepare<[string]>(`
  SELECT result_summary FROM runs
  WHERE task_id = ? AND status = 'success' AND result_summary IS NOT NULL
  ORDER BY id DESC
  LIMIT 1
`);

const sweepStatement = db.prepare<{ at: string }>(`
  UPDATE runs
  SET status = 'failed',
      error = 'Interrupted: the server stopped while this run was in flight.',
      finished_at = @at
  WHERE status = 'running'
`);

const pruneStatement = db.prepare<{ cutoff: string }>(
  `DELETE FROM runs WHERE started_at < @cutoff`
);

/**
 * A run row is written before the task executes, so a crash mid-run leaves a
 * dangling 'running' row. Resolve those at boot — otherwise they stay "in
 * progress" forever in the dashboard.
 */
export function sweepInterruptedRuns(): number {
  return sweepStatement.run({ at: now() }).changes;
}

export function pruneOldRuns(retentionDays: number = config.retentionDays): number {
  const cutoff = new Date(
    Date.now() - retentionDays * 24 * 60 * 60 * 1000
  ).toISOString();
  return pruneStatement.run({ cutoff }).changes;
}

export function startRun({ taskId, triggerSource }: StartRunInput): number {
  const info = insertRun.run({
    task_id: taskId,
    started_at: now(),
    trigger_source: triggerSource,
  });
  return Number(info.lastInsertRowid);
}

export function completeRun({
  id,
  status,
  conditionMet = false,
  notified = false,
  result,
  error = null,
}: CompleteRunInput): void {
  updateRun.run({
    id,
    finished_at: now(),
    status,
    condition_met: fromBoolean(conditionMet),
    notified: fromBoolean(notified),
    result_summary: result === undefined ? null : safeStringify(result),
    error,
  });
}

export function getRun(id: number): Run | null {
  const row = selectRun.get(id) as RunRow | undefined;
  return row ? toRun(row) : null;
}

/**
 * The stored result of this task's most recent successful run, or null if it
 * has never had one.
 *
 * Failed runs are skipped deliberately: an error says nothing about what the
 * page holds, so comparing against one would report a change that never
 * happened. Null is the honest answer for a task that has not yet succeeded,
 * and the `changed` operator treats it as "no change".
 */
export function getLastSuccessfulResult(taskId: string): unknown {
  const row = selectLastSuccessfulResult.get(taskId) as
    | { result_summary: string | null }
    | undefined;

  if (!row?.result_summary) return null;

  try {
    return JSON.parse(row.result_summary);
  } catch {
    return null;
  }
}

/** Keyed by task id, for decorating the task list. */
export function getLatestRunByTask(): Map<string, Run> {
  const rows = selectLatestPerTask.all() as RunRow[];
  return new Map(rows.map((row) => [row.task_id, toRun(row)]));
}

export interface ListRunsOptions {
  taskId?: string | undefined;
  status?: RunStatus | undefined;
  limit?: number;
  offset?: number;
}

export interface ListRunsResult {
  total: number;
  runs: Run[];
  limit: number;
  offset: number;
}

/**
 * Paginated run history. Filters are optional and compose; `total` reflects
 * them so the dashboard can page correctly.
 */
export function listRuns({
  taskId,
  status,
  limit = 50,
  offset = 0,
}: ListRunsOptions = {}): ListRunsResult {
  const where: string[] = [];
  const params: Record<string, string> = {};

  if (taskId) {
    where.push("runs.task_id = @taskId");
    params["taskId"] = taskId;
  }
  if (status) {
    where.push("runs.status = @status");
    params["status"] = status;
  }
  const clause = where.length ? `WHERE ${where.join(" AND ")}` : "";

  const { n: total } = db
    .prepare(`SELECT COUNT(*) AS n FROM runs ${clause}`)
    .get(params) as { n: number };

  const rows = db
    .prepare(
      `SELECT runs.*, tasks.name AS task_name
       FROM runs LEFT JOIN tasks ON tasks.id = runs.task_id
       ${clause}
       ORDER BY runs.id DESC
       LIMIT @limit OFFSET @offset`
    )
    .all({ ...params, limit, offset }) as RunRow[];

  return { total, runs: rows.map(toRun), limit, offset };
}
