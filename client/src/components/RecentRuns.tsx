import { Link } from "@tanstack/react-router";
import { Badge, Card, Text } from "brake-ui";
import { ArrowRight } from "lucide-react";
import type { Run } from "../types/index.ts";
import { StatusBadge } from "./StatusBadge.tsx";
import { NotificationBadge } from "./NotificationBadge.tsx";
import { useRuns } from "../hooks/useRuns.ts";
import { RECENT_RUNS_LIMIT } from "../constants/index.ts";
import { absoluteTime, firstLine, summarize, timeAgo } from "../utils/format.ts";

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

  return (
    <section className="flex flex-col gap-4 overflow-hidden">
      <div className="flex items-baseline justify-between">
        <Text variant="h4" as="h2">
          Recent runs
        </Text>
        <Link
          to="/runs"
          search={{ offset: 0 }}
          className="flex items-center gap-1 text-sm text-text-muted hover:text-text"
        >
          View all
          <ArrowRight size={14} aria-hidden="true" />
        </Link>
      </div>

      <Card size="lg" className="flex-1 overflow-hidden">
        <div className="overflow-auto">
          <table className="w-full min-w-md border-collapse text-left text-sm">
            <thead className="sticky top-0 z-10 bg-surface">
              <tr className="border-b border-border">
                {["When", "Task", "Status", "Result"].map((heading) => (
                  <th
                    key={heading}
                    className="px-3 py-2 font-medium whitespace-nowrap text-text-muted"
                  >
                    {heading}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {!isLoading && (runs?.runs.length ?? 0) === 0 && (
                <tr>
                  <td colSpan={4} className="px-3 py-8 text-center">
                    <Text color="muted">No runs yet.</Text>
                  </td>
                </tr>
              )}

              {(runs?.runs ?? []).map((run) => (
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
                    <div className="flex items-center gap-1.5">
                      <StatusBadge status={run.status} />
                      <NotificationBadge run={run} />
                      {run.condition_met &&
                        !run.notified &&
                        !run.notification_status && (
                          <Badge variant="info">met</Badge>
                        )}
                    </div>
                  </td>
                  <td className="max-w-56 px-3 py-2">
                    <span className="block truncate font-mono text-xs text-text-secondary">
                      {run.status === "failed"
                        ? firstLine(run.error)
                        : summarize(run.result_summary)}
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
