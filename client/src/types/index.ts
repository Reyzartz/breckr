/** Terminal state of a run. `running` is written before execution starts. */
export type RunStatus = "running" | "success" | "failed";

/** How a run was started. */
export type TriggerSource = "cron" | "manual";

/**
 * Why an alert did or did not go out.
 *
 * `disabled` and `error` are both "nothing arrived", but they are different
 * problems: `disabled` means the task has no channels attached, `error` means it
 * does and every one of them failed. Only `error` will be retried on the next
 * run.
 */
export type NotificationReason = "sent" | "disabled" | "error";

/** Which transport delivers a channel's alerts. */
export type ChannelType =
  | "telegram"
  | "discord"
  | "slack"
  | "webhook"
  | "email";

/**
 * A delivery destination the user manages.
 *
 * `config` is always the *redacted* view: secrets come back masked to their last
 * four characters and are never returned in full, so the form treats them as
 * write-only — an untouched masked field means "keep what is stored".
 */
export interface Channel {
  id: string;
  name: string;
  type: ChannelType;
  enabled: boolean;
  config: Record<string, unknown>;
  /**
   * True when the stored credentials could not be decrypted — almost always a
   * replaced key file. The channel keeps its name so it can be identified and
   * re-entered.
   */
  broken: boolean;
  created_at: string;
  updated_at: string;
}

/**
 * One channel's outcome for one run.
 *
 * `channel_id` goes null once the channel is deleted, but the name and type are
 * copies — history stays readable after the destination is gone.
 */
export interface NotificationAttempt {
  id: number;
  run_id: number;
  channel_id: string | null;
  channel_name: string;
  channel_type: ChannelType;
  status: NotificationReason;
  detail: string | null;
  message: string | null;
  attempted_at: string;
}

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
  /**
   * Why an alert did or did not go out, or null when none was owed because the
   * condition did not transition on this run. `notified` says whether one
   * arrived; this says why it did not, which a bool cannot.
   */
  notification_status: NotificationReason | null;
  /** The failure reason when `notification_status` is "error" or "disabled". */
  notification_detail: string | null;
  /** The alert body handed to the notifier, so what was sent is inspectable. */
  notification_message: string | null;
  /** Joined from tasks; null if the task row has since been removed. */
  task_name?: string | null;
  /**
   * Per-channel breakdown behind the aggregate above. Only present on
   * GET /api/runs/:id — the list view does not pay for it.
   */
  attempts?: NotificationAttempt[];
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
 * How a task's conditions combine into the single true/false that drives the
 * alert. One mode for the whole task — there is no nesting.
 */
export type MatchMode = "all" | "any";

/**
 * One thing to watch on the page.
 *
 * Every condition in a task reads the same page — the URL lives on the spec.
 * Watching two sites is two tasks.
 */
export interface Condition {
  selector: string;
  /** Waited for before extraction. Defaults to `selector` when omitted. */
  waitForSelector?: string;
  extract: ExtractKind;
  /** Required when `extract` is "attribute", ignored otherwise. */
  attribute?: string;
  operator: CompareOperator;
  /** Required except for "is_true", "is_false" and "changed". */
  value?: string;
}

/**
 * A task's behavior, declared rather than coded.
 *
 * Interpreted at run time by the executor, so nothing here is ever evaluated as
 * code — which is what makes it safe to author from the dashboard.
 */
export interface TaskSpec {
  /** http/https only; it is handed straight to a real browser. */
  url: string;
  /** How `conditions` combine. Omitted means "all". */
  match?: MatchMode;
  /**
   * At least one, at most MAX_CONDITIONS. Order is the order they are checked
   * and the order {{value1}}, {{value2}} … refer to.
   *
   * The server accepts the flat single-condition shape this replaced and hoists
   * it into a one-element list, so a stored task written before this existed
   * still opens in the form.
   */
  conditions: Condition[];
  /**
   * Alert body. Supports {{value}}, {{raw}}, {{url}}, {{name}} and the indexed
   * {{value1}} / {{raw1}} … one pair per condition.
   */
  message?: string;
}

/** What one condition saw on one run. */
export interface CheckResult {
  /** Identifies the condition that produced it, so history survives a reorder. */
  key: string;
  /** The typed extraction: number, string, or boolean depending on the kind. */
  value: number | string | boolean;
  /** The untouched text the value was derived from. */
  raw: string;
  /** Whether this condition's operator matched. */
  met: boolean;
}

/** What a spec-driven run returns, and what is stored as the run's result. */
export interface TaskResult {
  /** The first condition's extraction, repeated so {{value}} keeps its meaning. */
  value: number | string | boolean;
  /** The first condition's untouched text, for the same reason. */
  raw: string;
  url: string;
  /** ISO-8601. */
  checkedAt: string;
  /**
   * One entry per condition, in spec order. Absent on a result stored before
   * conditions became a list.
   */
  checks?: CheckResult[];
}

// --- Schedules -------------------------------------------------------------

/** Which shape a `Schedule` takes. `custom` is the raw-cron escape hatch. */
export type ScheduleFrequency =
  | "minutes"
  | "hours"
  | "day"
  | "week"
  | "month"
  | "custom";

/**
 * A schedule as the dashboard builds it, before it becomes cron.
 *
 * Cron stays the storage format: the server converts a `Schedule` to
 * `cron_expr` on the way in and derives one back from the stored expression on
 * the way out, so nothing but the server ever handles a cron string.
 *
 * `custom` exists because that derivation has to be total. An expression the
 * builder cannot express — a hand-written row, a range, the six-field form
 * node-cron also accepts — comes back as `custom` and survives an edit
 * untouched, rather than being silently rewritten into the nearest shape the
 * builder does have a control for.
 */
export type Schedule =
  /** `*​/N * * * *` */
  | { every: "minutes"; interval: number }
  /** `M *​/N * * *` */
  | { every: "hours"; interval: number; minute: number }
  /** `M H * * *` */
  | { every: "day"; hour: number; minute: number }
  /** `M H * * 1,5`, weekdays as cron's 0-6 with 0 = Sunday. */
  | { every: "week"; weekdays: number[]; hour: number; minute: number }
  /** `M H D * *`. Months without day `D` are skipped, as cron does. */
  | { every: "month"; day: number; hour: number; minute: number }
  | { every: "custom"; cron: string };

/**
 * When a task alerts, given a condition that is met.
 *
 * `transition` is the default because a condition that stays true is the normal
 * case for a monitor — a price that dropped stays dropped — and alerting on
 * every interval would train you to ignore the alerts. `always` exists for the
 * tasks where each matching run is its own event.
 */
export type NotifyMode =
  /** Only on the false → true edge, and not again until it goes back to false. */
  | "transition"
  /** On every run whose condition is met. */
  | "always";

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
   *
   * Tracked even under `always`, which ignores it — switching back to
   * `transition` has to land on the real state of the condition.
   */
  condition_met: boolean;
  /** When to alert while the condition is met. Defaults to `transition`. */
  notify_mode: NotifyMode;
  /** ISO-8601 of the last delivered alert, or null if none has been sent. */
  last_notified_at: string | null;
}

/** A task decorated with scheduling and history for the dashboard. */
export interface TaskWithStatus extends Task {
  /**
   * `cron_expr` in the shape the form's builder edits. Derived on read and
   * never stored, so a row whose expression was written by hand still opens in
   * the form — as `custom`, carrying the expression through unchanged.
   */
  schedule: Schedule;
  last_run: Run | null;
  /** ISO-8601 of the next scheduled fire, or null while disabled. */
  next_run: string | null;
  /**
   * True when the row carries no usable spec — it keeps its history but can no
   * longer be run, and the dashboard offers only deletion.
   */
  orphaned: boolean;
  /**
   * Channels this task alerts to, as saved. Includes disabled ones: the form
   * shows the links the user made, not the ones that would deliver right now.
   */
  channel_ids: string[];
}

// --- Responses -------------------------------------------------------------

export interface RunsResponse {
  total: number;
  runs: Run[];
  limit: number;
  offset: number;
}

/**
 * Whether this server asks for a password, and whether this browser has already
 * given it.
 *
 * `required: false` means no password is configured at all — the dashboard then
 * renders no login page and no logout button, which is the state every local
 * development run and every loopback deployment is in.
 */
export interface AuthStatusResponse {
  required: boolean;
  authenticated: boolean;
}

export interface LoginRequest {
  password: string;
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
  /**
   * Whether alerts can be delivered at all. Counted rather than probed — a real
   * probe would message the user's chat every time health was checked.
   */
  notifications: {
    /** True when at least one enabled channel exists. */
    configured: boolean;
    channels: number;
  };
  tasks: number;
  timezone: string;
}

/**
 * Outcome of a channel test: one real delivery attempt, on demand.
 *
 * Always returned with 200 — a rejection by the transport is a successful
 * report of a failed delivery, not an HTTP error.
 */
export interface TestNotificationResponse {
  ok: boolean;
  status: NotificationReason;
  /** Why it did not arrive. Present when not delivered. */
  detail?: string;
  /** Echoed so the dashboard can show exactly what was sent. */
  message: string;
  /** ISO-8601 of the attempt. */
  attemptedAt: string;
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

/**
 * Answer to POST /api/tasks/:id/run-now.
 *
 * The route does not wait for the run: it reports only that one was started,
 * and the run itself arrives over the event socket as it appears and then
 * resolves. There is no outcome to report yet at this point.
 */
export interface RunAcceptedResponse {
  accepted: boolean;
}

// --- Events ----------------------------------------------------------------

/** One slice of dashboard state, as named by a change event. */
export type MonitorResource = "tasks" | "runs" | "health" | "channels";

/**
 * What the server pushes over /api/events.
 *
 * Deliberately a signal and not a payload — the client refetches the named
 * resources through the same routes it already uses, which keeps run filtering,
 * pagination and totals server-side rather than reimplemented here.
 */
export interface ChangeEvent {
  type: "changed";
  resources: MonitorResource[];
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
  /**
   * The schedule to run on. Exactly one of `schedule` and `cron_expr` is
   * required; `schedule` wins when both are sent. The dashboard sends this one.
   */
  schedule?: Schedule;
  /**
   * Standard 5-field cron, evaluated in the server's configured timezone.
   * Kept for callers driving the API directly.
   */
  cron_expr?: string;
  spec: TaskSpec;
  /** When to alert while the condition is met. Defaults to `transition`. */
  notify_mode?: NotifyMode;
  /** Defaults to true. */
  enabled?: boolean;
  /** Channels to alert on. Empty means the task records history but never alerts. */
  channel_ids?: string[];
}

/** Every field is optional; only what is present is changed. */
export interface UpdateTaskRequest {
  enabled?: boolean;
  name?: string;
  /** Takes precedence over `cron_expr` when both are sent. */
  schedule?: Schedule;
  cron_expr?: string;
  spec?: TaskSpec;
  /**
   * Absent leaves the mode alone. Changing it does not re-arm the trigger: the
   * condition is unchanged, so the stored state still describes it.
   */
  notify_mode?: NotifyMode;
  /** Absent leaves the links alone; `[]` detaches every channel. */
  channel_ids?: string[];
}

/**
 * Creates a channel. `config` is shaped by `type` — see CHANNEL_FIELDS in
 * constants for what each transport needs.
 */
export interface CreateChannelRequest {
  name: string;
  type: ChannelType;
  config: Record<string, unknown>;
  /** Defaults to true. */
  enabled?: boolean;
}

/**
 * Patches a channel; only what is present is changed.
 *
 * An omitted `config` keeps the stored credentials, and within a submitted
 * config a blank or still-masked field keeps its stored value — so a rename
 * never costs a token the dashboard was never shown.
 */
export interface UpdateChannelRequest {
  name?: string;
  config?: Record<string, unknown>;
  enabled?: boolean;
}

/** A channel config tested before it has been saved. */
export interface TestChannelRequest {
  type: ChannelType;
  config: Record<string, unknown>;
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
