import test from "node:test";
import assert from "node:assert/strict";
import type { TaskResult, TaskSpec } from "@breckr/shared";
import { evaluateCondition, renderMessage } from "./executor.service.ts";

/**
 * The condition and message half of the executor — everything except the page.
 *
 * A spec is interpreted, not evaluated, so this is where "does `lt` actually
 * mean less-than" is pinned down. No browser and no database are involved.
 */

function spec(overrides: Partial<TaskSpec>): TaskSpec {
  return {
    url: "https://example.com",
    selector: ".price",
    extract: "number",
    operator: "lt",
    ...overrides,
  };
}

function result(value: TaskResult["value"], raw = String(value)): TaskResult {
  return { value, raw, url: "https://example.com", checkedAt: "2026-01-01T00:00:00Z" };
}

const numeric: [operator: TaskSpec["operator"], value: string, at: number, expected: boolean][] =
  [
    ["lt", "100", 99, true],
    ["lt", "100", 100, false],
    ["lte", "100", 100, true],
    ["gt", "100", 101, true],
    ["gt", "100", 100, false],
    ["gte", "100", 100, true],
    ["eq", "100", 100, true],
    ["eq", "100", 101, false],
    ["neq", "100", 101, true],
  ];

for (const [operator, value, at, expected] of numeric) {
  test(`${operator} ${value} against ${String(at)} is ${String(expected)}`, () => {
    assert.equal(
      evaluateCondition(spec({ operator, value }), result(at)),
      expected
    );
  });
}

const textual: [operator: TaskSpec["operator"], value: string, at: string, expected: boolean][] =
  [
    ["contains", "stock", "In stock now", true],
    ["contains", "stock", "Sold out", false],
    ["not_contains", "Sold out", "In stock now", true],
    ["not_contains", "Sold out", "Sold out", false],
    ["eq", "In stock", "In stock", true],
    ["neq", "In stock", "Sold out", true],
  ];

for (const [operator, value, at, expected] of textual) {
  test(`${operator} "${value}" against "${at}" is ${String(expected)}`, () => {
    assert.equal(
      evaluateCondition(spec({ extract: "text", operator, value }), result(at)),
      expected
    );
  });
}

test("is_true and is_false read an existence check", () => {
  const present = spec({ extract: "exists", operator: "is_true" });
  const absent = spec({ extract: "exists", operator: "is_false" });

  assert.equal(evaluateCondition(present, result(true)), true);
  assert.equal(evaluateCondition(present, result(false)), false);
  assert.equal(evaluateCondition(absent, result(false)), true);
  assert.equal(evaluateCondition(absent, result(true)), false);
});

test("eq compares as strings so a page number matches a form string", () => {
  // The form always yields a string; a `number` extraction always yields a
  // number. Comparing them strictly would make eq permanently false.
  assert.equal(evaluateCondition(spec({ operator: "eq", value: "10" }), result(10)), true);
});

// --- changed ---------------------------------------------------------------

const changed = spec({ extract: "text", operator: "changed" });

test("changed is false on the very first run", () => {
  // Nothing to compare against, so a brand-new task must not alert on whatever
  // it happens to see first.
  assert.equal(evaluateCondition(changed, result("18:25:43"), undefined), false);
});

test("changed fires when the value differs from the last success", () => {
  assert.equal(evaluateCondition(changed, result("18:25:44"), "18:25:43"), true);
});

test("changed is false when the value held steady", () => {
  // This is what re-arms the edge-trigger: the run after a change sees no
  // change, so the next change fires again.
  assert.equal(evaluateCondition(changed, result("18:25:43"), "18:25:43"), false);
});

// --- messages --------------------------------------------------------------

test("renders every placeholder", () => {
  const message = renderMessage(
    spec({ message: "{{name}}: {{value}} (raw {{raw}}) at {{url}}" }),
    result(42, "$42.00"),
    "Price check"
  );

  assert.equal(message, "Price check: 42 (raw $42.00) at https://example.com");
});

test("tolerates whitespace inside a placeholder", () => {
  assert.equal(
    renderMessage(spec({ message: "now {{ value }}" }), result(7), "T"),
    "now 7"
  );
});

test("falls back to a default body when no template is set", () => {
  const message = renderMessage(spec({}), result(42), "Price check");

  assert.match(message, /Price check/);
  assert.match(message, /42/);
});

test("a template is substituted, never evaluated", () => {
  // The whole point of the declarative spec: no user string reaches a function
  // constructor, so this stays literal text.
  const message = renderMessage(
    spec({ message: "${process.exit(1)} and {{value}}" }),
    result(1),
    "T"
  );

  assert.equal(message, "${process.exit(1)} and 1");
});
