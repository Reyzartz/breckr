import type { Page } from "puppeteer-core";
import type { TaskResult, TaskSpec } from "@breckr/shared";
import { config } from "../config/index.ts";
import {
  MESSAGE_PLACEHOLDER_PATTERN,
  SELECTOR_TIMEOUT_MS,
} from "../constants/index.ts";
import * as runs from "../repositories/runs.repository.ts";
import type { ResolvedTask } from "../types/index.ts";

/**
 * Turns a declarative spec into something the runner can execute.
 *
 * The spec is *interpreted*, never evaluated — no user string is ever passed to
 * a function constructor or a `vm`. That is what makes it safe to author a task
 * from a dashboard that has no authentication in front of it.
 *
 * The output is the same `ResolvedTask` the file-based registry used to produce,
 * so the runner, the browser mutex, the notifier and the edge-trigger state
 * machine are all untouched by this change.
 */

/** What `run()` returned last time, for the `changed` operator. */
function previousValue(taskId: string): TaskResult["value"] | undefined {
  const previous = runs.getLastSuccessfulResult(taskId);
  if (typeof previous !== "object" || previous === null) return undefined;

  const { value } = previous as Partial<TaskResult>;
  return typeof value === "number" ||
    typeof value === "string" ||
    typeof value === "boolean"
    ? value
    : undefined;
}

/**
 * Pull a number out of whatever the page rendered — "$1,299.00", "1 299 kr".
 *
 * Throws rather than returning NaN: a selector that started matching a
 * different element would otherwise compare NaN against the threshold, which is
 * false for every operator. The monitor would look healthy and never fire.
 */
function parseNumber(raw: string, selector: string): number {
  const cleaned = raw.replace(/[^0-9.-]/g, "");
  const parsed = Number.parseFloat(cleaned);

  if (!Number.isFinite(parsed)) {
    throw new Error(
      `Could not parse a number from ${JSON.stringify(raw)} at "${selector}".`
    );
  }
  return parsed;
}

async function extractValue(
  page: Page,
  spec: TaskSpec
): Promise<{ value: TaskResult["value"]; raw: string }> {
  if (spec.extract === "exists") {
    const handle = await page.$(spec.selector);
    return { value: handle !== null, raw: handle === null ? "" : "present" };
  }

  if (spec.extract === "count") {
    const count = await page.$$eval(spec.selector, (elements) => elements.length);
    return { value: count, raw: String(count) };
  }

  if (spec.extract === "attribute") {
    // Non-null: validateSpec guarantees `attribute` whenever the kind is
    // "attribute", and specs are validated before they are ever stored.
    const attribute = spec.attribute as string;
    const raw = await page.$eval(
      spec.selector,
      (element, name) => element.getAttribute(name) ?? "",
      attribute
    );
    return { value: raw, raw };
  }

  const raw = (await page.$eval(spec.selector, (element) => element.textContent ?? ""))
    .trim();

  return spec.extract === "number"
    ? { value: parseNumber(raw, spec.selector), raw }
    : { value: raw, raw };
}

/**
 * `exists` and `count` are the two kinds that must not wait.
 *
 * Waiting for a selector that is *expected* to be absent would burn the
 * selector timeout on every run and then fail the run, which is exactly
 * backwards for "alert me when this appears".
 */
function shouldWait(spec: TaskSpec): boolean {
  if (spec.waitForSelector) return true;
  return spec.extract !== "exists" && spec.extract !== "count";
}

async function execute(page: Page, spec: TaskSpec): Promise<TaskResult> {
  await page.goto(spec.url, { waitUntil: "domcontentloaded" });

  if (shouldWait(spec)) {
    const target = spec.waitForSelector ?? spec.selector;
    // An explicit sub-timeout, so a selector that stopped matching fails as
    // "waiting for .price" rather than as a generic run timeout that says
    // nothing about which step stalled.
    const handle = await page.waitForSelector(target, {
      timeout: SELECTOR_TIMEOUT_MS,
    });
    if (!handle) {
      throw new Error(`No element matched "${target}" at ${spec.url}.`);
    }
  }

  const { value, raw } = await extractValue(page, spec);

  return { value, raw, url: spec.url, checkedAt: new Date().toISOString() };
}

/**
 * Apply the spec's operator to one extraction.
 *
 * `previous` is only consulted by `changed`; passing undefined means "no
 * successful run to compare against", which reads as no change — so a task
 * never alerts on the very first thing it sees.
 */
export function evaluateCondition(
  spec: TaskSpec,
  result: TaskResult,
  previous?: TaskResult["value"] | undefined
): boolean {
  const { value } = result;

  switch (spec.operator) {
    case "is_true":
      return value === true;
    case "is_false":
      return value === false;
    case "changed":
      return previous !== undefined && previous !== value;
    case "lt":
      return Number(value) < Number(spec.value);
    case "lte":
      return Number(value) <= Number(spec.value);
    case "gt":
      return Number(value) > Number(spec.value);
    case "gte":
      return Number(value) >= Number(spec.value);
    case "contains":
      return String(value).includes(String(spec.value));
    case "not_contains":
      return !String(value).includes(String(spec.value));
    case "eq":
      // Compared as strings so "10" from the page matches 10 from the form —
      // numeric kinds are already numbers, and everything else is text anyway.
      return String(value) === String(spec.value);
    case "neq":
      return String(value) !== String(spec.value);
  }
}

/** Render the alert body by substitution. Never evaluated as code. */
export function renderMessage(
  spec: TaskSpec,
  result: TaskResult,
  taskName: string
): string {
  if (!spec.message) {
    return `Task "${taskName}" matched: ${String(result.value)} (${spec.url})`;
  }

  const values: Record<string, string> = {
    value: String(result.value),
    raw: result.raw,
    url: result.url,
    name: taskName,
  };

  return spec.message.replace(
    MESSAGE_PLACEHOLDER_PATTERN,
    (whole, name: string) => values[name] ?? whole
  );
}

export interface CompilableTask {
  id: string;
  name: string;
  cron_expr: string;
  spec: TaskSpec;
}

/** Compile a stored task into the shape `runTask` consumes. */
export function compile(task: CompilableTask): ResolvedTask<TaskResult> {
  const { spec } = task;

  return {
    id: task.id,
    name: task.name,
    cron: task.cron_expr,
    timeoutMs: config.defaultTimeoutMs,
    // Every declarative spec reads a page, so the CDP connection is always
    // needed. `withoutPage` stays in the browser service for the tests.
    needsBrowser: true,
    run: (page) => execute(page, spec),
    condition: (result) =>
      evaluateCondition(spec, result, previousValue(task.id)),
    notify: (result) => renderMessage(spec, result, task.name),
  };
}

/**
 * Run a draft spec once, for the dashboard's "Test" button.
 *
 * Deliberately writes no run row and sends no notification: pressing Test while
 * getting a selector right must not pollute history, and must not alert anyone.
 * The `changed` operator has nothing to compare against here, so it reads false.
 */
export async function testSpec(
  page: Page,
  spec: TaskSpec,
  taskName: string
): Promise<{ result: TaskResult; conditionMet: boolean; message: string }> {
  const result = await execute(page, spec);
  const conditionMet = evaluateCondition(spec, result);

  return { result, conditionMet, message: renderMessage(spec, result, taskName) };
}
