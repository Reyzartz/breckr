/** Terminal state of a run. `running` is written before execution starts. */
export type RunStatus = "running" | "success" | "failed";

/** How a run was started. */
export type TriggerSource = "cron" | "manual";

/**
 * One execution of a task.
 *
 * `condition_met` and `notified` are real booleans: SQLite stores them as 0/1
 * and the runs repository converts them at the boundary, so nothing downstream
 * has to coerce.
 */
export interface Run {
  id: number;
  task_id: string;
  /** ISO-8601. Written before execution, so it always exists. */
  started_at: string;
  /** ISO-8601, or null while the run is still in flight. */
  finished_at: string | null;
  status: RunStatus;
  /** Whether the task's condition matched on this run. */
  condition_met: boolean;
  /** Whether an alert was actually delivered for this run. */
  notified: boolean;
  trigger_source: TriggerSource;
  /** JSON-encoded return value of the task's run(), or null when it failed. */
  result_summary: string | null;
  /** Message and stack when the run failed. */
  error: string | null;
  /** Joined from tasks; null if the task row has since been removed. */
  task_name?: string | null;
}

// --- Task specs ------------------------------------------------------------

/** What to pull out of the page once the selector has matched. */
export type ExtractKind = "text" | "number" | "attribute" | "count" | "exists";

/**
 * How the extracted value is tested. Not every operator applies to every
 * `ExtractKind` — the server rejects an invalid pairing when the task is saved,
 * rather than letting it surface as a condition that can never fire.
 */
export type CompareOperator =
  /** number, count */
  | "lt"
  | "lte"
  | "gt"
  | "gte"
  /** every kind */
  | "eq"
  | "neq"
  /** text, attribute */
  | "contains"
  | "not_contains"
  /** exists */
  | "is_true"
  | "is_false"
  /** every kind: differs from the last successful run */
  | "changed";

/**
 * A task's behavior, declared rather than coded.
 *
 * Interpreted at run time by the executor, so nothing here is ever evaluated as
 * code — which is what makes it safe to author from the dashboard.
 */
export interface TaskSpec {
  /** http/https only; it is handed straight to a real browser. */
  url: string;
  /** Waited for before extraction. Defaults to `selector` when omitted. */
  waitForSelector?: string;
  selector: string;
  extract: ExtractKind;
  /** Required when `extract` is "attribute", ignored otherwise. */
  attribute?: string;
  operator: CompareOperator;
  /** Required except for "is_true", "is_false" and "changed". */
  value?: string;
  /** Alert body. Supports {{value}}, {{raw}}, {{url}} and {{name}}. */
  message?: string;
}

/** What a spec-driven run returns, and what is stored as the run's result. */
export interface TaskResult {
  /** The typed extraction: number, string, or boolean depending on the kind. */
  value: number | string | boolean;
  /** The untouched text the value was derived from. */
  raw: string;
  url: string;
  /** ISO-8601. */
  checkedAt: string;
}

/** A task as stored. */
export interface Task {
  id: string;
  name: string;
  cron_expr: string;
  enabled: boolean;
  /**
   * Null only for a legacy row written before tasks moved into the database, or
   * one whose stored JSON no longer parses. Such a task keeps its history but
   * cannot be scheduled — see `TaskWithStatus.orphaned`.
   */
  spec: TaskSpec | null;
  /**
   * Last known result of the condition. Drives edge-triggering: an alert fires
   * only on the false -> true transition, and this persists across restarts so
   * a reboot cannot re-notify.
   */
  condition_met: boolean;
  /** ISO-8601 of the last delivered alert, or null if none has been sent. */
  last_notified_at: string | null;
}

/** A task decorated with scheduling and history for the dashboard. */
export interface TaskWithStatus extends Task {
  last_run: Run | null;
  /** ISO-8601 of the next scheduled fire, or null while disabled. */
  next_run: string | null;
  /**
   * True when the row carries no usable spec — it keeps its history but can no
   * longer be run, and the dashboard offers only deletion.
   */
  orphaned: boolean;
}

// --- Responses -------------------------------------------------------------

export interface TasksResponse {
  tasks: TaskWithStatus[];
}

export interface RunsResponse {
  total: number;
  runs: Run[];
  limit: number;
  offset: number;
}

export interface HealthResponse {
  ok: true;
  browser: {
    endpoint: string;
    reachable: boolean;
    /** Present when reachable. */
    version?: string;
    /** Present when not reachable. */
    error?: string;
  };
  tasks: number;
  timezone: string;
}

export interface UpdateTaskResponse {
  id: string;
  enabled: boolean;
  next_run: string | null;
}

/**
 * Outcome of POST /api/tasks/test: one execution of a draft spec.
 *
 * Writes no run row and sends no notification, so it can be pressed freely
 * while getting a selector right.
 */
export interface TestTaskResponse {
  ok: boolean;
  /** Present when ok. */
  result?: TaskResult;
  /** Whether the draft's condition matched this extraction. */
  conditionMet?: boolean;
  /** The alert body that would have been sent, rendered from the template. */
  message?: string;
  /** Present when the run failed — a bad selector, a timeout, a dead URL. */
  error?: string;
}

/** Outcome of a single run, returned by POST /api/tasks/:id/run-now. */
export interface RunOutcome {
  runId: number;
  status: RunStatus;
  conditionMet: boolean;
  notified: boolean;
  error?: string;
}

export interface ErrorResponse {
  error: string;
  /**
   * Present when a task spec failed validation: which field was wrong, so the
   * dashboard can show the message against that control rather than as a
   * page-level banner.
   */
  field?: string;
}

// --- Requests --------------------------------------------------------------

export interface CreateTaskRequest {
  /** Stable identifier. Run history is keyed on it, so it cannot be changed. */
  id: string;
  name: string;
  /** Standard 5-field cron, evaluated in the server's configured timezone. */
  cron_expr: string;
  spec: TaskSpec;
  /** Defaults to true. */
  enabled?: boolean;
}

/** Every field is optional; only what is present is changed. */
export interface UpdateTaskRequest {
  enabled?: boolean;
  name?: string;
  cron_expr?: string;
  spec?: TaskSpec;
}

/** A draft task, run once without being saved. */
export interface TestTaskRequest {
  /** Only used to render {{name}} in the message template. */
  name?: string;
  spec: TaskSpec;
}

export interface ListRunsQuery {
  task_id?: string;
  status?: RunStatus;
  limit?: number;
  offset?: number;
}
