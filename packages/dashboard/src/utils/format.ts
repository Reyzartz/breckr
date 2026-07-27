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

/** Compact one-line preview of a result for the history table. */
export function summarize(text: string | null | undefined, max = 80): string {
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
