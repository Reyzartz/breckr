import cron from "node-cron";
import type { Schedule } from "@breckr/shared";
import { SCHEDULE_FREQUENCIES } from "../constants/index.ts";
import { fail } from "../utils/errors.ts";

/**
 * The two directions between a form's schedule and a cron expression.
 *
 * Cron is the storage format and this is the only place that reads or writes
 * one on a task's behalf — the dashboard exchanges `Schedule` objects and never
 * builds an expression itself, so there is a single implementation of the
 * mapping rather than one per side that can drift.
 *
 * `fromCron` is total on purpose. Every stored row has to open in the form,
 * including expressions this module cannot express structurally, so anything
 * unrecognized comes back as `custom` carrying the original text. That is what
 * keeps editing a task's name from quietly rewriting its schedule.
 *
 * Pure: no database, no config, no clock.
 */

const FIELD = "schedule";

/** Cron's own day numbering, Sunday first. */
const WEEKDAY_MAX = 6;

function requireInt(value: unknown, label: string, min: number, max: number): number {
  if (typeof value !== "number" || !Number.isInteger(value) || value < min || value > max) {
    fail(
      FIELD,
      `${label} must be a whole number between ${min} and ${max}, got ${JSON.stringify(value)}.`
    );
  }
  return value;
}

function requireWeekdays(value: unknown): number[] {
  if (!Array.isArray(value) || value.length === 0) {
    fail(FIELD, "`weekdays` must list at least one day.");
  }

  const days = new Set<number>();
  for (const day of value) {
    days.add(requireInt(day, "`weekdays`", 0, WEEKDAY_MAX));
  }
  // Sorted and deduped so two forms of the same week produce one expression,
  // and so fromCron's output compares equal to what went in.
  return [...days].sort((a, b) => a - b);
}

/**
 * Convert a schedule to cron, validating it on the way.
 *
 * This is also the validator for untrusted input: every field is range-checked
 * here, so a body that type-asserts its way to `Schedule` still cannot produce
 * an expression node-cron would reject at schedule time.
 */
export function toCron(schedule: Schedule): string {
  switch (schedule.every) {
    case "minutes": {
      const interval = requireInt(schedule.interval, "`interval`", 1, 59);
      return `*/${interval} * * * *`;
    }

    case "hours": {
      const interval = requireInt(schedule.interval, "`interval`", 1, 23);
      const minute = requireInt(schedule.minute, "`minute`", 0, 59);
      return `${minute} */${interval} * * *`;
    }

    case "day": {
      const hour = requireInt(schedule.hour, "`hour`", 0, 23);
      const minute = requireInt(schedule.minute, "`minute`", 0, 59);
      return `${minute} ${hour} * * *`;
    }

    case "week": {
      const weekdays = requireWeekdays(schedule.weekdays);
      const hour = requireInt(schedule.hour, "`hour`", 0, 23);
      const minute = requireInt(schedule.minute, "`minute`", 0, 59);
      return `${minute} ${hour} * * ${weekdays.join(",")}`;
    }

    case "month": {
      const day = requireInt(schedule.day, "`day`", 1, 31);
      const hour = requireInt(schedule.hour, "`hour`", 0, 23);
      const minute = requireInt(schedule.minute, "`minute`", 0, 59);
      return `${minute} ${hour} ${day} * *`;
    }

    case "custom": {
      if (typeof schedule.cron !== "string" || !schedule.cron.trim()) {
        fail(FIELD, "`cron` must be a non-empty string.");
      }
      const expr = schedule.cron.trim();
      if (!cron.validate(expr)) {
        fail(FIELD, `"${expr}" is not a valid cron expression.`);
      }
      return expr;
    }

    default:
      fail(
        FIELD,
        `\`every\` must be one of ${SCHEDULE_FREQUENCIES.join(", ")}, got "${String((schedule as { every: unknown }).every)}".`
      );
  }
}

/** A bare non-negative integer. Ranges, lists and steps deliberately fail. */
function plainInt(field: string): number | null {
  return /^\d+$/.test(field) ? Number(field) : null;
}

/** The `*​/N` step form, or null. */
function stepOf(field: string): number | null {
  const captured = /^\*\/(\d+)$/.exec(field)?.[1];
  return captured === undefined ? null : Number(captured);
}

/** Expand a day-of-week field's lists and ranges, or null if it uses anything else. */
function parseWeekdays(field: string): number[] | null {
  const days = new Set<number>();

  for (const part of field.split(",")) {
    const range = /^(\d)-(\d)$/.exec(part);
    if (range) {
      const from = Number(range[1]);
      const to = Number(range[2]);
      // Cron allows a wrapping range like 5-1; the builder has no way to show
      // one, so it stays custom rather than being silently reordered.
      if (from > to || to > WEEKDAY_MAX) return null;
      for (let day = from; day <= to; day += 1) days.add(day);
      continue;
    }

    const single = plainInt(part);
    // Names (MON) and 7-for-Sunday are valid cron but have no control here.
    if (single === null || single > WEEKDAY_MAX) return null;
    days.add(single);
  }

  return days.size === 0 ? null : [...days].sort((a, b) => a - b);
}

/**
 * Derive the schedule a cron expression came from.
 *
 * Total: anything outside the five shapes above — a six-field pattern, a month
 * restriction, a step in the day fields, weekday names, junk — comes back as
 * `custom` with the text untouched.
 */
export function fromCron(expr: string): Schedule {
  const custom: Schedule = { every: "custom", cron: expr };

  const fields = expr.trim().split(/\s+/);
  if (fields.length !== 5) return custom;
  // The defaults are unreachable after the length check; they exist so the
  // elements type as string under noUncheckedIndexedAccess.
  const [minute = "", hour = "", dom = "", month = "", dow = ""] = fields;

  // Every structured shape runs in every month.
  if (month !== "*") return custom;

  const everyDayOfMonth = dom === "*";
  const everyWeekday = dow === "*";

  if (everyDayOfMonth && everyWeekday) {
    // "Every minute" and "every hour at :M" are the unstepped spellings of an
    // interval of 1. Saving them rewrites the expression to the step form,
    // which is the same schedule and reads far better than "Custom".
    if (hour === "*") {
      if (minute === "*") return { every: "minutes", interval: 1 };

      const minuteStep = stepOf(minute);
      if (minuteStep !== null) return { every: "minutes", interval: minuteStep };
    }

    const atMinute = plainInt(minute);
    if (atMinute === null) return custom;

    if (hour === "*") return { every: "hours", interval: 1, minute: atMinute };

    const hourStep = stepOf(hour);
    if (hourStep !== null) {
      return { every: "hours", interval: hourStep, minute: atMinute };
    }

    const atHour = plainInt(hour);
    if (atHour === null) return custom;
    return { every: "day", hour: atHour, minute: atMinute };
  }

  const atMinute = plainInt(minute);
  const atHour = plainInt(hour);
  if (atMinute === null || atHour === null) return custom;

  if (everyDayOfMonth) {
    const weekdays = parseWeekdays(dow);
    if (weekdays === null) return custom;
    return { every: "week", weekdays, hour: atHour, minute: atMinute };
  }

  if (everyWeekday) {
    const day = plainInt(dom);
    // Cron treats day 0 as invalid; leave it to node-cron to reject.
    if (day === null || day < 1 || day > 31) return custom;
    return { every: "month", day, hour: atHour, minute: atMinute };
  }

  // Both day fields constrained: cron ORs them, which no single control shows.
  return custom;
}

/** Shape-check an untrusted `schedule` from a request body and resolve its cron. */
export function validateSchedule(raw: unknown): string {
  if (typeof raw !== "object" || raw === null || Array.isArray(raw)) {
    fail(FIELD, "`schedule` must be an object.");
  }
  return toCron(raw as Schedule);
}
