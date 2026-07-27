import type {
  CompareOperator,
  ExtractKind,
  RunStatus,
  ScheduleFrequency,
  TriggerSource,
} from "@breckr/shared";

/**
 * Runtime values live here rather than in @breckr/shared, which is types-only:
 * Node cannot strip types inside node_modules, so a runtime export there would
 * fail at boot. Typing them against the shared types keeps the two in step —
 * adding a status to the contract breaks this file until it is handled.
 */
export const RUN_STATUSES: readonly RunStatus[] = [
  "running",
  "success",
  "failed",
];

export const TRIGGER_SOURCES: readonly TriggerSource[] = ["cron", "manual"];

// --- API pagination --------------------------------------------------------

export const DEFAULT_RUN_LIMIT = 50;
/** Upper bound on `limit`, so one request cannot pull the whole history. */
export const MAX_RUN_LIMIT = 200;

// --- Scheduling ------------------------------------------------------------

/** Retention sweep: daily at 04:00 in the configured timezone. */
export const RETENTION_CRON = "0 4 * * *";
export const RETENTION_JOB_NAME = "_retention";

/** The schedule shapes the dashboard's builder can send. */
export const SCHEDULE_FREQUENCIES: readonly ScheduleFrequency[] = [
  "minutes",
  "hours",
  "day",
  "week",
  "month",
  "custom",
];

// --- Telegram --------------------------------------------------------------

export const TELEGRAM_API_BASE = "https://api.telegram.org";
export const TELEGRAM_TIMEOUT_MS = 10_000;
/** Telegram rejects messages longer than this outright. */
export const TELEGRAM_MAX_MESSAGE_LENGTH = 4096;
export const TELEGRAM_TRUNCATION_SUFFIX = "\n… (truncated)";

// --- Browser ---------------------------------------------------------------

/** Timeout for the /api/health liveness probe, shorter than a real run. */
export const BROWSER_PROBE_TIMEOUT_MS = 5_000;

// --- Task specs ------------------------------------------------------------

/** Task ids appear in URLs, so keep them boring. */
export const TASK_ID_PATTERN = /^[a-zA-Z0-9._-]+$/;

export const EXTRACT_KINDS: readonly ExtractKind[] = [
  "text",
  "number",
  "attribute",
  "count",
  "exists",
];

/**
 * Which operators make sense for each kind.
 *
 * The pairing is enforced when a task is saved rather than when it runs: `gt`
 * on an `exists` check would otherwise be a condition that can never fire, and
 * a monitor that quietly never fires is the failure this app exists to avoid.
 */
export const OPERATORS_BY_KIND: Readonly<
  Record<ExtractKind, readonly CompareOperator[]>
> = {
  text: ["eq", "neq", "contains", "not_contains", "changed"],
  number: ["lt", "lte", "gt", "gte", "eq", "neq", "changed"],
  attribute: ["eq", "neq", "contains", "not_contains", "changed"],
  count: ["lt", "lte", "gt", "gte", "eq", "neq", "changed"],
  exists: ["is_true", "is_false", "changed"],
};

/** Operators that test the value on its own, so `spec.value` is not needed. */
export const VALUELESS_OPERATORS: readonly CompareOperator[] = [
  "is_true",
  "is_false",
  "changed",
];

/** Kinds whose `spec.value` must parse as a finite number. */
export const NUMERIC_KINDS: readonly ExtractKind[] = ["number", "count"];

/** Placeholders the message template may reference. */
export const MESSAGE_PLACEHOLDERS = ["value", "raw", "url", "name"] as const;

/** Captures `{{name}}`, tolerating inner whitespace. */
export const MESSAGE_PLACEHOLDER_PATTERN = /\{\{\s*(\w+)\s*\}\}/g;

/**
 * How long to wait for the selector, well under DEFAULT_TIMEOUT_MS so a
 * selector that stopped matching fails as "waiting for .price" rather than as a
 * generic run timeout that says nothing about which step stalled.
 */
export const SELECTOR_TIMEOUT_MS = 10_000;
