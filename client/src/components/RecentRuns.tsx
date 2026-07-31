import { Link } from "@tanstack/react-router";
import { Card, Text } from "brake-ui";
import { ArrowRight } from "lucide-react";
import type { Run } from "../types/index.ts";
import { RunBadges, RunListItem, runOutcome } from "./RunSummary.tsx";
import { useRuns } from "../hooks/useRuns.ts";
import { RECENT_RUNS_LIMIT } from "../constants/index.ts";
import { absoluteTime, timeAgo } from "../utils/format.ts";

interface RecentRunsProps {
  onSelectRun: (run: Run) => void;
}

/**
 * The dashboard's compact run panel: the last few runs, unfiltered, with
 * nothing to page through — the full table with filters and pagination is
 * what /runs is for.
 */
export function RecentRuns({ onSelectRun }: RecentRunsProps) {
  const { runs, isLoading } = useRuns({ offset: 0, limit: RECENT_RUNS_LIMIT });
  const rows = runs?.runs ?? [];
  const isEmpty = !isLoading && rows.length === 0;

  return (
    <section className="flex min-w-0 flex-col gap-3 xl:overflow-hidden">
      <div className="flex items-baseline justify-between gap-2">
        <Text variant="h3" as="h2">
          Recent runs
        </Text>
        <Link
          to="/runs"
          search={{ offset: 0 }}
          className="flex items-center gap-1 rounded-md py-1 text-sm text-text-muted transition-colors hover:text-text"
        >
          View all
          <ArrowRight size={14} aria-hidden="true" />
        </Link>
      </div>

      {/*
        Below md the table becomes a stack of cards. Four columns on a phone
        is a horizontal scroll that hides the result — the one column you
        opened the panel to read.
      */}
      <div className="grid grid-cols-1 gap-2 md:hidden">
        {isEmpty && (
          <Card>
            <Text color="muted">No runs yet.</Text>
          </Card>
        )}
        {rows.map((run) => (
          <RunListItem key={run.id} run={run} onSelect={onSelectRun} />
        ))}
      </div>

      <Card size="lg" className="hidden md:block xl:min-h-0 xl:flex-1 xl:overflow-hidden">
        <div className="h-full overflow-auto">
          <table className="w-full min-w-md border-collapse text-left text-sm">
            <thead className="sticky top-0 z-10 bg-surface">
              <tr className="border-b border-border">
                {["When", "Task", "Status", "Result"].map((heading) => (
                  <th
                    key={heading}
                    // Result absorbs whatever is left, so a wide panel shows
                    // more of the value instead of padding the row out.
                    className={`px-3 py-2 font-medium whitespace-nowrap text-text-muted ${
                      heading === "Result" ? "w-full" : ""
                    }`}
                  >
                    {heading}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {isEmpty && (
                <tr>
                  <td colSpan={4} className="px-3 py-8 text-center">
                    <Text color="muted">No runs yet.</Text>
                  </td>
                </tr>
              )}

              {rows.map((run) => (
                <tr
                  key={run.id}
                  onClick={() => {
                    onSelectRun(run);
                  }}
                  className="cursor-pointer border-b border-border transition-colors last:border-b-0 hover:bg-surface-hover"
                >
                  <td
                    className="px-3 py-2 whitespace-nowrap"
                    title={absoluteTime(run.started_at)}
                  >
                    {timeAgo(run.started_at)}
                  </td>
                  <td className="px-3 py-2 whitespace-nowrap">
                    {run.task_name ?? run.task_id}
                  </td>
                  <td className="px-3 py-2 whitespace-nowrap">
                    <RunBadges run={run} />
                  </td>
                  {/* w-full + max-w-0 is what makes truncate work in an
                      auto-layout table: the cell takes the leftover width but
                      reports no minimum, so the span clips instead of pushing. */}
                  <td className="w-full max-w-0 px-3 py-2">
                    <span className="block truncate font-mono text-xs text-text-secondary">
                      {runOutcome(run)}
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </Card>
    </section>
  );
}
