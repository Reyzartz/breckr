import cron from "node-cron";
import type { CompareOperator, ExtractKind, TaskSpec } from "@breckr/shared";
import {
  EXTRACT_KINDS,
  MESSAGE_PLACEHOLDERS,
  MESSAGE_PLACEHOLDER_PATTERN,
  NUMERIC_KINDS,
  OPERATORS_BY_KIND,
  TASK_ID_PATTERN,
  VALUELESS_OPERATORS,
} from "../constants/index.ts";
import { fail } from "../utils/errors.ts";
import { validateSchedule } from "./schedule.service.ts";

/**
 * Validation for user-authored task specs.
 *
 * Specs arrive over HTTP from the dashboard, so this is the only thing standing
 * between a typo and a monitor that silently never fires. Every rejection has to
 * say what to fix — the message is shown against the offending field in the
 * form.
 *
 * Pure: no database, no browser, no config. That is what makes the whole
 * rejection table testable without either.
 */

// Re-exported so callers keep importing the rejection type from the validator
// that raises it; it lives in utils/ because schedule.service raises it too.
export { SpecValidationError } from "../utils/errors.ts";

function requireString(value: unknown, field: string, label: string): string {
  if (typeof value !== "string" || !value.trim()) {
    fail(field, `${label} must be a non-empty string.`);
  }
  return value.trim();
}

/** Optional strings normalize to undefined rather than "", so absent is absent. */
function optionalString(value: unknown, field: string, label: string): string | undefined {
  if (value === undefined || value === null || value === "") return undefined;
  if (typeof value !== "string") {
    fail(field, `${label} must be a string when present.`);
  }
  return value.trim() || undefined;
}

/**
 * A spec's URL is handed straight to a real browser, so the scheme has to be
 * checked here: `file:` would read the container's filesystem and `javascript:`
 * would execute in the page — neither is something a monitor should reach.
 */
function validateUrl(raw: unknown): string {
  const value = requireString(raw, "url", "`url`");

  let parsed: URL;
  try {
    parsed = new URL(value);
  } catch {
    fail("url", `\`url\` "${value}" is not a valid absolute URL.`);
  }

  if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
    fail("url", `\`url\` must be http:// or https://, got "${parsed.protocol}".`);
  }

  return parsed.toString();
}

function validateExtract(raw: unknown): ExtractKind {
  if (typeof raw !== "string" || !EXTRACT_KINDS.includes(raw as ExtractKind)) {
    fail(
      "extract",
      `\`extract\` must be one of ${EXTRACT_KINDS.join(", ")}, got "${String(raw)}".`
    );
  }
  return raw as ExtractKind;
}

function validateOperator(raw: unknown, extract: ExtractKind): CompareOperator {
  const allowed = OPERATORS_BY_KIND[extract];

  if (typeof raw !== "string") {
    fail("operator", "`operator` must be a string.");
  }
  if (!allowed.includes(raw as CompareOperator)) {
    fail(
      "operator",
      `\`operator\` "${raw}" cannot be used with extract "${extract}". Allowed: ${allowed.join(", ")}.`
    );
  }
  return raw as CompareOperator;
}

/**
 * An unknown placeholder is rejected rather than rendered literally: `{{prive}}`
 * would otherwise ship in the alert body as-is, and you would only find out at
 * the moment you most wanted the message to be right.
 */
function validateMessage(raw: unknown): string | undefined {
  const message = optionalString(raw, "message", "`message`");
  if (message === undefined) return undefined;

  for (const [, name] of message.matchAll(MESSAGE_PLACEHOLDER_PATTERN)) {
    if (!MESSAGE_PLACEHOLDERS.includes(name as (typeof MESSAGE_PLACEHOLDERS)[number])) {
      fail(
        "message",
        `\`message\` references unknown placeholder {{${String(name)}}}. Available: ${MESSAGE_PLACEHOLDERS.map((p) => `{{${p}}}`).join(", ")}.`
      );
    }
  }

  return message;
}

/** Validate a spec and return it normalized, with blanks collapsed away. */
export function validateSpec(candidate: unknown): TaskSpec {
  if (typeof candidate !== "object" || candidate === null) {
    fail("spec", "`spec` must be an object.");
  }

  const input = candidate as Partial<TaskSpec>;

  const url = validateUrl(input.url);
  const selector = requireString(input.selector, "selector", "`selector`");
  const waitForSelector = optionalString(
    input.waitForSelector,
    "waitForSelector",
    "`waitForSelector`"
  );
  const extract = validateExtract(input.extract);
  const operator = validateOperator(input.operator, extract);
  const message = validateMessage(input.message);

  // Only meaningful for `attribute`, and dropped otherwise so a leftover value
  // from switching kinds in the form does not linger in the stored spec.
  let attribute: string | undefined;
  if (extract === "attribute") {
    attribute = requireString(
      input.attribute,
      "attribute",
      '`attribute` is required when extract is "attribute" and'
    );
  }

  let value: string | undefined;
  if (!VALUELESS_OPERATORS.includes(operator)) {
    value = requireString(
      input.value,
      "value",
      `\`value\` is required for operator "${operator}" and`
    );

    if (NUMERIC_KINDS.includes(extract) && !Number.isFinite(Number(value))) {
      fail(
        "value",
        `\`value\` must be a number when extract is "${extract}", got "${value}".`
      );
    }
  }

  return {
    url,
    selector,
    extract,
    operator,
    ...(waitForSelector === undefined ? {} : { waitForSelector }),
    ...(attribute === undefined ? {} : { attribute }),
    ...(value === undefined ? {} : { value }),
    ...(message === undefined ? {} : { message }),
  };
}

export interface ValidatedTaskInput {
  id: string;
  name: string;
  cron_expr: string;
  spec: TaskSpec;
}

export function validateTaskId(raw: unknown): string {
  const id = requireString(raw, "id", "`id`");
  if (!TASK_ID_PATTERN.test(id)) {
    fail("id", `\`id\` "${id}" must contain only letters, digits, . _ or -.`);
  }
  return id;
}

export function validateName(raw: unknown): string {
  return requireString(raw, "name", "`name`");
}

export function validateCron(raw: unknown): string {
  const expr = requireString(raw, "cron_expr", "`cron_expr`");
  if (!cron.validate(expr)) {
    fail("cron_expr", `\`cron_expr\` "${expr}" is not a valid cron expression.`);
  }
  return expr;
}

/**
 * Resolve the cron a request means, from whichever field it used.
 *
 * The dashboard sends a structured `schedule`; a caller driving the API by hand
 * can still send `cron_expr`. `schedule` wins so a client that sends both is
 * not silently scheduled on the stale one.
 */
export function resolveCron(schedule: unknown, cronExpr: unknown): string {
  if (schedule !== undefined && schedule !== null) return validateSchedule(schedule);
  if (cronExpr !== undefined && cronExpr !== null) return validateCron(cronExpr);
  fail("schedule", "A `schedule` or a `cron_expr` is required.");
}

/** The whole envelope, for POST /api/tasks. */
export function validateTaskInput(candidate: unknown): ValidatedTaskInput {
  if (typeof candidate !== "object" || candidate === null) {
    fail("body", "Request body must be an object.");
  }

  const input = candidate as Record<string, unknown>;

  return {
    id: validateTaskId(input["id"]),
    name: validateName(input["name"]),
    cron_expr: resolveCron(input["schedule"], input["cron_expr"]),
    spec: validateSpec(input["spec"]),
  };
}
