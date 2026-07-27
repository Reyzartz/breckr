import test from "node:test";
import assert from "node:assert/strict";
import cron from "node-cron";
import type { Schedule } from "@breckr/shared";
import { fromCron, toCron, validateSchedule } from "./schedule.service.ts";
import { SpecValidationError } from "../utils/errors.ts";

/**
 * The dashboard never sees a cron string, so this mapping is the only thing
 * keeping "every day at 09:00" in the form and `0 9 * * *` in the database
 * describing the same schedule.
 *
 * Two properties matter more than any single case:
 *
 * - every expression this emits is one node-cron will actually accept, and
 * - `fromCron` undoes `toCron` exactly, or a task's schedule would drift a
 *   little every time someone opened the form to fix a typo in its name.
 */

const schedules: [name: string, schedule: Schedule, expected: string][] = [
  ["every minute", { every: "minutes", interval: 1 }, "*/1 * * * *"],
  ["quarter-hourly", { every: "minutes", interval: 15 }, "*/15 * * * *"],
  ["every 2 hours at :30", { every: "hours", interval: 2, minute: 30 }, "30 */2 * * *"],
  ["daily at 09:05", { every: "day", hour: 9, minute: 5 }, "5 9 * * *"],
  [
    "weekly on one day",
    { every: "week", weekdays: [1], hour: 8, minute: 0 },
    "0 8 * * 1",
  ],
  [
    "weekly on several days",
    { every: "week", weekdays: [1, 3, 5], hour: 8, minute: 0 },
    "0 8 * * 1,3,5",
  ],
  [
    "weekly on Sunday",
    { every: "week", weekdays: [0], hour: 23, minute: 59 },
    "59 23 * * 0",
  ],
  ["monthly", { every: "month", day: 1, hour: 3, minute: 0 }, "0 3 1 * *"],
  ["custom", { every: "custom", cron: "0 9 */2 * *" }, "0 9 */2 * *"],
];

for (const [name, schedule, expected] of schedules) {
  test(`converts ${name} to cron`, () => {
    const expr = toCron(schedule);

    assert.equal(expr, expected);
    assert.ok(cron.validate(expr), "node-cron has to accept what we emit");
  });

  test(`round-trips ${name} back from cron`, () => {
    assert.deepEqual(fromCron(toCron(schedule)), schedule);
  });
}

test("sorts and dedupes weekdays so one week has one expression", () => {
  assert.equal(
    toCron({ every: "week", weekdays: [5, 1, 5], hour: 8, minute: 0 }),
    "0 8 * * 1,5"
  );
});

/**
 * The unstepped spellings of an interval of 1. Saving one rewrites it to the
 * step form — the same schedule, and far better than showing "Custom" for
 * something the builder can plainly express.
 */
const equivalents: [expr: string, schedule: Schedule][] = [
  ["* * * * *", { every: "minutes", interval: 1 }],
  ["30 * * * *", { every: "hours", interval: 1, minute: 30 }],
];

for (const [expr, schedule] of equivalents) {
  test(`reads "${expr}" as its step form`, () => {
    assert.deepEqual(fromCron(expr), schedule);
  });
}

test("expands a weekday range", () => {
  assert.deepEqual(fromCron("0 7 * * 1-5"), {
    every: "week",
    weekdays: [1, 2, 3, 4, 5],
    hour: 7,
    minute: 0,
  });
});

test("expands mixed weekday lists and ranges", () => {
  assert.deepEqual(fromCron("0 7 * * 1-2,5"), {
    every: "week",
    weekdays: [1, 2, 5],
    hour: 7,
    minute: 0,
  });
});

/**
 * Everything the builder has no control for. These have to survive an edit
 * untouched rather than being rounded to the nearest shape that does have one.
 */
const unrepresentable: [name: string, expr: string][] = [
  ["six fields", "* * * * * *"],
  ["too few fields", "*/15 * * *"],
  ["a month restriction", "0 9 1 6 *"],
  ["a step in the day of month", "0 9 */2 * *"],
  ["a step in the weekday", "0 9 * * */2"],
  ["a weekday name", "0 9 * * MON"],
  ["7 for Sunday", "0 9 * * 7"],
  ["a wrapping weekday range", "0 9 * * 5-1"],
  ["both day fields constrained", "0 9 1 * 1"],
  ["an hour range", "0 9-17 * * *"],
  ["a minute list", "0,30 9 * * *"],
  ["junk", "not a cron"],
];

for (const [name, expr] of unrepresentable) {
  test(`keeps ${name} as custom`, () => {
    assert.deepEqual(fromCron(expr), { every: "custom", cron: expr });
  });
}

test("a custom schedule survives a round trip through the form", () => {
  const stored = "0 9 */2 * *";
  // What the form would send back after being opened and saved unchanged.
  assert.equal(toCron(fromCron(stored)), stored);
});

const rejections: [name: string, input: unknown, expected: RegExp][] = [
  ["not an object", "nope", /`schedule` must be an object/],
  ["an array", [], /`schedule` must be an object/],
  ["an unknown frequency", { every: "fortnight" }, /`every` must be one of/],
  [
    "an interval below the range",
    { every: "minutes", interval: 0 },
    /`interval` must be a whole number between 1 and 59/,
  ],
  [
    "an interval above the range",
    { every: "minutes", interval: 60 },
    /`interval` must be a whole number between 1 and 59/,
  ],
  [
    "more than 23 hours",
    { every: "hours", interval: 24, minute: 0 },
    /`interval` must be a whole number between 1 and 23/,
  ],
  [
    "a non-integer",
    { every: "day", hour: 9.5, minute: 0 },
    /`hour` must be a whole number between 0 and 23/,
  ],
  [
    "a missing number",
    { every: "day", hour: 9 },
    /`minute` must be a whole number between 0 and 59/,
  ],
  ["no weekdays", { every: "week", weekdays: [], hour: 9, minute: 0 }, /at least one day/],
  [
    "a weekday out of range",
    { every: "week", weekdays: [7], hour: 9, minute: 0 },
    /`weekdays` must be a whole number between 0 and 6/,
  ],
  [
    "day 0 of the month",
    { every: "month", day: 0, hour: 9, minute: 0 },
    /`day` must be a whole number between 1 and 31/,
  ],
  ["an empty custom cron", { every: "custom", cron: "  " }, /must be a non-empty string/],
  [
    "an invalid custom cron",
    { every: "custom", cron: "not a cron" },
    /is not a valid cron expression/,
  ],
];

for (const [name, input, expected] of rejections) {
  test(`rejects ${name}`, () => {
    assert.throws(
      () => validateSchedule(input),
      (err: unknown) => {
        assert.ok(err instanceof SpecValidationError);
        // The form renders this against the builder, so it has to be the
        // group's name and not one of the individual controls.
        assert.equal(err.field, "schedule");
        assert.match(err.message, expected);
        return true;
      }
    );
  });
}
