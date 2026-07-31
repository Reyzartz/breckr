import type { Schedule } from "../types/index.ts";
import { WEEKDAY_LABELS } from "../constants/index.ts";

const UNITS: [Intl.RelativeTimeFormatUnit, number][] = [
  ["year", 365 * 24 * 60 * 60 * 1000],
  ["day", 24 * 60 * 60 * 1000],
  ["hour", 60 * 60 * 1000],
  ["minute", 60 * 1000],
  ["second", 1000],
];

const relative = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });

/** "3 minutes ago" / "in 42 seconds" — for past timestamps and next-run alike. */
export function timeAgo(iso: string | null | undefined): string {
  if (!iso) return "—";
  const delta = new Date(iso).getTime() - Date.now();
  if (!Number.isFinite(delta)) return "—";

  for (const [unit, ms] of UNITS) {
    if (Math.abs(delta) >= ms || unit === "second") {
      return relative.format(Math.round(delta / ms), unit);
    }
  }
  return "just now";
}

export function absoluteTime(iso: string | null | undefined): string {
  if (!iso) return "—";
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return "—";
  return date.toLocaleString();
}

/** Wall-clock duration of a run, or null while it is still in flight. */
export function duration(
  startedAt: string | null | undefined,
  finishedAt: string | null | undefined
): string | null {
  if (!startedAt || !finishedAt) return null;
  const ms = new Date(finishedAt).getTime() - new Date(startedAt).getTime();
  if (!Number.isFinite(ms) || ms < 0) return null;

  if (ms < 1000) return `${String(ms)}ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`;
  return `${String(Math.floor(ms / 60_000))}m ${String(Math.round((ms % 60_000) / 1000))}s`;
}

/** Pretty-print stored JSON, leaving non-JSON summaries untouched. */
export function prettyJson(text: string | null | undefined): string {
  if (!text) return "";
  try {
    return JSON.stringify(JSON.parse(text), null, 2);
  } catch {
    return text;
  }
}

/**
 * Compact one-line preview of a result for the history table.
 *
 * The cap is a safety bound on how much of a stored blob reaches the DOM, not
 * the visual truncation — every caller clips with `truncate`, so the width of
 * the column decides what is actually shown. Keeping it well above what any
 * column can fit is what lets a wide table use the room it has instead of
 * ellipsing at a width chosen for a phone.
 */
export function summarize(text: string | null | undefined, max = 300): string {
  if (!text) return "—";

  let compact = text;
  try {
    compact = JSON.stringify(JSON.parse(text));
  } catch {
    // Not JSON — show it as-is.
  }

  return compact.length > max ? `${compact.slice(0, max)}…` : compact;
}

/** First line only — stack traces are for the detail modal, not the table. */
export function firstLine(text: string | null | undefined): string {
  if (!text) return "—";
  return text.split("\n")[0] ?? "—";
}

/**
 * Host of a URL, falling back to the raw string.
 *
 * Specs are validated on save, so a stored URL should always parse — but a row
 * edited by hand must not be able to throw inside a render and take the whole
 * task list down with it.
 */
export function hostname(url: string): string {
  try {
    return new URL(url).hostname;
  } catch {
    return url;
  }
}

/**
 * "Price check" -> "price-check", for suggesting a task id from its name.
 *
 * Only the characters the server's TASK_ID_PATTERN accepts survive, so the
 * suggestion is always valid — the user can still overwrite it.
 */
export function slugify(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

const pad = (value: number): string => String(value).padStart(2, "0");

/**
 * A schedule in words, for the task list.
 *
 * Formats the structured schedule the server derived — no cron is parsed here,
 * so the card and the form's builder cannot disagree about what a task does.
 * A `custom` schedule has no words to give, so it yields its raw expression;
 * callers render that one as code.
 */
export function describeSchedule(schedule: Schedule): string {
  switch (schedule.every) {
    case "minutes":
      return schedule.interval === 1
        ? "Every minute"
        : `Every ${schedule.interval} minutes`;
    case "hours":
      return schedule.interval === 1
        ? `Hourly at :${pad(schedule.minute)}`
        : `Every ${schedule.interval} hours at :${pad(schedule.minute)}`;
    case "day":
      return `Daily at ${pad(schedule.hour)}:${pad(schedule.minute)}`;
    case "week": {
      const days = schedule.weekdays.map((day) => WEEKDAY_LABELS[day] ?? day).join(", ");
      return `${days} at ${pad(schedule.hour)}:${pad(schedule.minute)}`;
    }
    case "month":
      return `Monthly on day ${schedule.day} at ${pad(schedule.hour)}:${pad(schedule.minute)}`;
    case "custom":
      return schedule.cron;
  }
}
