import { Badge, Text } from "brake-ui";
import type { Run } from "../types/index.ts";
import { StatusBadge } from "./StatusBadge.tsx";
import { NotificationBadge } from "./NotificationBadge.tsx";
import { absoluteTime, duration, firstLine, summarize, timeAgo } from "../utils/format.ts";

/**
 * The status cluster for one run.
 *
 * Shared so the table and the mobile card cannot drift into telling the same
 * run's story two different ways.
 */
export function RunBadges({ run }: { run: Run }) {
  return (
    /*
      Wrapping is for the card, which has a phone's width to work with. The
      table only renders from md up, where a wrapped status cell would just
      make every row in the column taller.
    */
    <div className="flex flex-wrap items-center gap-1.5 md:flex-nowrap">
      <StatusBadge status={run.status} />
      <NotificationBadge run={run} />
      {/*
        Only when no alert was owed at all -- otherwise the notification badge
        already says what happened, and a failed delivery would read as a
        plain "met".
      */}
      {run.condition_met && !run.notified && !run.notification_status && (
        <Badge variant="info">met</Badge>
      )}
    </div>
  );
}

/** What the run produced, or why it failed. */
export function runOutcome(run: Run): string {
  return run.status === "failed" ? firstLine(run.error) : summarize(run.result_summary);
}

interface RunListItemProps {
  run: Run;
  onSelect: (run: Run) => void;
  /** Duration and trigger, which the compact dashboard panel leaves out. */
  detailed?: boolean;
}

/**
 * One run as a card, for viewports too narrow for the table.
 *
 * A six-column table on a phone is a horizontal scroll with the important
 * columns off-screen, so below `md` the same rows are stacked instead: task
 * first, then how it went, then when and what it produced.
 *
 * It is a real button rather than a clickable row, which also gets the run
 * detail onto the keyboard path the table never had.
 */
export function RunListItem({ run, onSelect, detailed = false }: RunListItemProps) {
  const outcome = runOutcome(run);
  const meta = [
    duration(run.started_at, run.finished_at),
    run.trigger_source === "manual" ? "manual" : null,
  ].filter((part): part is string => part !== null);

  return (
    <button
      type="button"
      onClick={() => {
        onSelect(run);
      }}
      className="w-full cursor-pointer rounded-lg border border-border px-3 py-3 text-left transition-colors hover:bg-surface-hover focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-border-focus"
    >
      <div className="flex items-start justify-between gap-2">
        <Text variant="h5" as="span" className="min-w-0 truncate">
          {run.task_name ?? run.task_id}
        </Text>
        <Text
          variant="small"
          color="muted"
          as="span"
          className="shrink-0 whitespace-nowrap"
        >
          <span title={absoluteTime(run.started_at)}>{timeAgo(run.started_at)}</span>
        </Text>
      </div>

      <div className="mt-2">
        <RunBadges run={run} />
      </div>

      {/* Both formatters yield an em dash when there is nothing to show, which
          is a column placeholder — a card just omits the line instead. */}
      {outcome !== "—" && (
        <span className="mt-2 block truncate font-mono text-xs text-text-secondary">
          {outcome}
        </span>
      )}

      {detailed && meta.length > 0 && (
        <Text variant="small" color="muted" as="span" className="mt-1.5 block">
          {meta.join(" · ")}
        </Text>
      )}
    </button>
  );
}
