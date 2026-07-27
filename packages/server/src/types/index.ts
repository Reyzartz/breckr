import type { Page } from "puppeteer-core";
import type { TriggerSource, RunStatus } from "@breckr/shared";

/**
 * Minimal structural logger. Both `console` and Fastify's pino logger satisfy
 * it, so services can be called from a request, from cron, or from a test
 * without caring which is which.
 */
export interface Logger {
  info(obj: object, msg?: string): void;
  warn(obj: object, msg?: string): void;
  error(obj: object, msg?: string): void;
}

/**
 * An executable task, as the runner sees it.
 *
 * Tasks are authored from the dashboard as declarative `TaskSpec`s and turned
 * into this shape by `executor.service.compile()`. Nothing constructs one by
 * hand any more, but the runner is still written against this interface rather
 * than against the spec — which is what kept the run pipeline, the browser
 * mutex and the edge-trigger state machine unchanged when tasks moved out of
 * files and into the database.
 */
export interface TaskDefinition<TResult = unknown> {
  /** Stable identifier. Run history is keyed on it, so renaming loses history. */
  id: string;
  name: string;
  /** Standard 5-field cron, evaluated in the configured timezone. */
  cron: string;
  /** Overrides DEFAULT_TIMEOUT_MS. Covers connect and execution together. */
  timeoutMs?: number;
  /**
   * Set false for a task that needs no page at all — the CDP connection is
   * skipped entirely, which is how the pipeline stays testable with no browser.
   * Every declarative spec reads a page, so in practice only tests set this.
   */
  needsBrowser?: boolean;
  /** Extract what you want to watch. Must return something JSON-serializable. */
  run(page: Page): Promise<TResult> | TResult;
  /** True when the event you care about has happened. Edge-triggered. */
  condition?(result: TResult): boolean;
  /** Message body for the alert. Required whenever `condition` is present. */
  notify?(result: TResult): string;
}

/**
 * A definition after validation, with defaults applied.
 *
 * Optional fields become required, and the two callbacks become explicitly
 * `null` rather than absent — so downstream code branches on a value instead of
 * having to distinguish "not provided" from "provided as undefined".
 */
export interface ResolvedTask<TResult = unknown>
  extends Omit<
    TaskDefinition<TResult>,
    "timeoutMs" | "needsBrowser" | "condition" | "notify"
  > {
  timeoutMs: number;
  needsBrowser: boolean;
  condition: ((result: TResult) => boolean) | null;
  notify: ((result: TResult) => string) | null;
}

/**
 * Why a notification did or did not go out.
 *
 * The caller must tell `error` from `disabled` apart: `error` still owes an
 * alert and must leave the edge-trigger disarmed so the next run retries, while
 * `disabled` owes nothing and advances state as if delivered.
 */
export type NotificationReason = "sent" | "disabled" | "error";

export interface NotificationOutcome {
  delivered: boolean;
  reason: NotificationReason;
}

export interface RunRecord {
  runId: number;
  status: RunStatus;
  conditionMet: boolean;
  notified: boolean;
  error?: string;
}

export interface StartRunInput {
  taskId: string;
  triggerSource: TriggerSource;
}

export interface CompleteRunInput {
  id: number;
  status: RunStatus;
  conditionMet?: boolean;
  notified?: boolean;
  result?: unknown;
  error?: string | null;
}
