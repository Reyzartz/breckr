import type {
  CompareOperator,
  ExtractKind,
  NotificationReason,
  RunStatus,
  Schedule,
  ScheduleFrequency,
  TaskSpec,
} from "../types/index.ts";

/** Runs per page in the history table. */
export const PAGE_SIZE = 25;

/**
 * Runtime values live here, not in `../types`, which is the types-only mirror
 * of the server's contract. Typing them against it keeps the two in step —
 * `server/internal/types` stays the authority, this copy just stops the form
 * offering a pairing the server would reject.
 */
export const RUN_STATUSES: readonly RunStatus[] = ["success", "failed", "running"];

/** brake-ui Badge variants, keyed by run status. */
export const STATUS_BADGE_VARIANT: Record<RunStatus, "success" | "error" | "info"> = {
  success: "success",
  failed: "error",
  running: "info",
};

/**
 * brake-ui Badge variants, keyed by notification outcome.
 *
 * `error` is red and `disabled` is neutral on purpose: an alert the transport
 * rejected is a fault to fix, while an alert nothing was configured to send is
 * just the state of the install.
 */
export const NOTIFICATION_BADGE_VARIANT: Record<
  NotificationReason,
  "success" | "error" | "default"
> = {
  sent: "success",
  disabled: "default",
  error: "error",
};

/** How each outcome reads in the UI. */
export const NOTIFICATION_LABEL: Record<NotificationReason, string> = {
  sent: "notified",
  disabled: "not configured",
  error: "notify failed",
};

// --- Task form -------------------------------------------------------------

/**
 * The extraction kinds, labelled for the form.
 *
 * The server holds the same list in its own `constants/`; both are typed
 * against `ExtractKind`, so adding a kind to the shared contract breaks
 * whichever side has not been updated.
 */
export const EXTRACT_OPTIONS: readonly { value: ExtractKind; label: string }[] = [
  { value: "text", label: "Text of the element" },
  { value: "number", label: "Number in the text" },
  { value: "attribute", label: "An attribute" },
  { value: "count", label: "How many match" },
  { value: "exists", label: "Whether it exists" },
];

export const OPERATOR_LABELS: Record<CompareOperator, string> = {
  lt: "is less than",
  lte: "is at most",
  gt: "is greater than",
  gte: "is at least",
  eq: "equals",
  neq: "does not equal",
  contains: "contains",
  not_contains: "does not contain",
  is_true: "is present",
  is_false: "is missing",
  changed: "changed since the last run",
};

/**
 * Which operators the form offers per kind. Mirrors OPERATORS_BY_KIND on the
 * server, which is the authority — this only keeps the user from picking a
 * pairing that would be rejected on save.
 */
export const OPERATORS_BY_KIND: Record<ExtractKind, readonly CompareOperator[]> = {
  text: ["eq", "neq", "contains", "not_contains", "changed"],
  number: ["lt", "lte", "gt", "gte", "eq", "neq", "changed"],
  attribute: ["eq", "neq", "contains", "not_contains", "changed"],
  count: ["lt", "lte", "gt", "gte", "eq", "neq", "changed"],
  exists: ["is_true", "is_false", "changed"],
};

/** Operators that test the value alone, so the form hides the value input. */
export const VALUELESS_OPERATORS: readonly CompareOperator[] = [
  "is_true",
  "is_false",
  "changed",
];

// --- Schedule builder ------------------------------------------------------

/** A sensible starting point for a new task: check quarter-hourly. */
export const DEFAULT_SCHEDULE: Schedule = { every: "minutes", interval: 15 };

/**
 * The frequencies the builder offers, in the order they appear.
 *
 * `custom` is last because it is the escape hatch: the server hands one back
 * for any stored expression the other five cannot express, and editing such a
 * task has to leave its cron alone.
 */
export const FREQUENCY_OPTIONS: readonly { value: ScheduleFrequency; label: string }[] =
  [
    { value: "minutes", label: "Every few minutes" },
    { value: "hours", label: "Every few hours" },
    { value: "day", label: "Every day" },
    { value: "week", label: "Every week" },
    { value: "month", label: "Every month" },
    { value: "custom", label: "Custom cron" },
  ];

/** Indexed by cron's day numbering, Sunday first. */
export const WEEKDAY_LABELS: readonly string[] = [
  "Sun",
  "Mon",
  "Tue",
  "Wed",
  "Thu",
  "Fri",
  "Sat",
];

/** Upper bound per frequency, matching the server's own range checks. */
export const MAX_INTERVAL: Record<"minutes" | "hours", number> = {
  minutes: 59,
  hours: 23,
};

export const DEFAULT_SPEC: TaskSpec = {
  url: "",
  selector: "",
  extract: "text",
  operator: "changed",
};

export const THEMES = ["light", "dark"] as const;
export type Theme = (typeof THEMES)[number];

/** brake-ui switches theme off this attribute on any ancestor. */
export const THEME_ATTRIBUTE = "data-theme";
export const THEME_STORAGE_KEY = "breckr-theme";
