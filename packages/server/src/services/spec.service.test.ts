import test from "node:test";
import assert from "node:assert/strict";
import type { TaskSpec } from "@breckr/shared";
import {
  SpecValidationError,
  validateSpec,
  validateTaskInput,
} from "./spec.service.ts";

/**
 * Specs arrive over HTTP from a form, so validation is the only thing standing
 * between a typo and a monitor that silently never fires. Every case here must
 * be rejected with a message that says what to fix, and name the field so the
 * dashboard can point at it.
 */

const valid: TaskSpec = {
  url: "https://example.com/prices",
  selector: ".price",
  extract: "number",
  operator: "lt",
  value: "100",
};

test("accepts a minimal spec and normalizes it", () => {
  const spec = validateSpec(valid);

  assert.equal(spec.selector, ".price");
  assert.equal(spec.operator, "lt");
  assert.equal(spec.waitForSelector, undefined, "absent optionals stay absent");
  assert.equal(spec.message, undefined);
});

test("drops blank optionals rather than storing empty strings", () => {
  const spec = validateSpec({ ...valid, waitForSelector: "  ", message: "" });

  assert.equal(spec.waitForSelector, undefined);
  assert.equal(spec.message, undefined);
});

test("drops a stale attribute when the kind does not use one", () => {
  // Switching `extract` in the form leaves the old attribute in the payload;
  // storing it would be misleading noise in a spec that never reads it.
  const spec = validateSpec({ ...valid, attribute: "href" });

  assert.equal(spec.attribute, undefined);
});

test("value-less operators need no value", () => {
  const spec = validateSpec({
    url: "https://example.com",
    selector: "#banner",
    extract: "exists",
    operator: "is_true",
  });

  assert.equal(spec.value, undefined);
});

test("accepts every documented placeholder", () => {
  const spec = validateSpec({
    ...valid,
    message: "{{name}}: {{value}} (raw {{raw}}) at {{url}}",
  });

  assert.match(spec.message ?? "", /\{\{value\}\}/);
});

const rejections: [name: string, spec: unknown, field: string, expected: RegExp][] = [
  ["not an object", 42, "spec", /`spec` must be an object/],
  ["missing url", { ...valid, url: "" }, "url", /`url` must be a non-empty string/],
  ["unparseable url", { ...valid, url: "not a url" }, "url", /is not a valid absolute URL/],
  [
    "file scheme",
    { ...valid, url: "file:///etc/passwd" },
    "url",
    /must be http:\/\/ or https:\/\//,
  ],
  [
    "javascript scheme",
    { ...valid, url: "javascript:alert(1)" },
    "url",
    /must be http:\/\/ or https:\/\//,
  ],
  ["missing selector", { ...valid, selector: "" }, "selector", /`selector` must be/],
  [
    "unknown extract kind",
    { ...valid, extract: "innerHTML" },
    "extract",
    /`extract` must be one of/,
  ],
  [
    "operator that cannot apply to the kind",
    { ...valid, extract: "exists", operator: "gt" },
    "operator",
    /cannot be used with extract "exists"/,
  ],
  [
    "attribute kind without an attribute",
    { ...valid, extract: "attribute", operator: "eq", value: "x", attribute: "" },
    "attribute",
    /`attribute` is required/,
  ],
  [
    "missing value for an operator that needs one",
    { ...valid, value: undefined },
    "value",
    /`value` is required for operator "lt"/,
  ],
  [
    "non-numeric value on a numeric kind",
    { ...valid, value: "cheap" },
    "value",
    /`value` must be a number when extract is "number"/,
  ],
  [
    "unknown message placeholder",
    { ...valid, message: "Price is {{prive}}" },
    "message",
    /unknown placeholder \{\{prive\}\}/,
  ],
];

for (const [name, spec, field, expected] of rejections) {
  test(`rejects: ${name}`, () => {
    assert.throws(
      () => validateSpec(spec),
      (err: unknown) => {
        assert.ok(err instanceof SpecValidationError, "must be a validation error");
        assert.equal(err.field, field, "names the offending field");
        assert.match(err.message, expected);
        return true;
      }
    );
  });
}

// --- The envelope ----------------------------------------------------------

const validInput = {
  id: "price-check",
  name: "Price check",
  cron_expr: "*/15 * * * *",
  spec: valid,
};

test("accepts a complete task input", () => {
  const input = validateTaskInput(validInput);

  assert.equal(input.id, "price-check");
  assert.equal(input.spec.selector, ".price");
});

const envelopeRejections: [name: string, input: unknown, field: string, expected: RegExp][] =
  [
    ["not an object", "nope", "body", /must be an object/],
    ["missing id", { ...validInput, id: "" }, "id", /`id` must be a non-empty string/],
    [
      "id with spaces",
      { ...validInput, id: "has space" },
      "id",
      /must contain only letters/,
    ],
    ["missing name", { ...validInput, name: "  " }, "name", /`name` must be/],
    [
      "invalid cron",
      { ...validInput, cron_expr: "not a cron" },
      "cron_expr",
      /is not a valid cron expression/,
    ],
  ];

for (const [name, input, field, expected] of envelopeRejections) {
  test(`rejects envelope: ${name}`, () => {
    assert.throws(
      () => validateTaskInput(input),
      (err: unknown) => {
        assert.ok(err instanceof SpecValidationError);
        assert.equal(err.field, field);
        assert.match(err.message, expected);
        return true;
      }
    );
  });
}
