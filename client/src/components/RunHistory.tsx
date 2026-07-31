import { Button, Card, Select, Text } from "brake-ui";
import { ChevronLeft, ChevronRight } from "lucide-react";
import type { Run, RunStatus, RunsResponse, TaskWithStatus } from "../types/index.ts";
import { RunBadges, RunListItem, runOutcome } from "./RunSummary.tsx";
import { PAGE_SIZE, RUN_STATUSES } from "../constants/index.ts";
import type { RunFilters } from "../hooks/useRuns.ts";
import { absoluteTime, duration, timeAgo } from "../utils/format.ts";

/** Result carries the flexible width; the rest size to their content. */
const COLUMNS = ["When", "Task", "Status", "Result", "Took", "Trigger"] as const;

interface RunHistoryProps {
  data: RunsResponse | null;
  tasks: TaskWithStatus[];
  filters: RunFilters;
  onFilterChange: (patch: Partial<Omit<RunFilters, "offset">>) => void;
  onNextPage: () => void;
  onPreviousPage: () => void;
  onSelectRun: (run: Run) => void;
  loading: boolean;
}

export function RunHistory({
  data,
  tasks,
  filters,
  onFilterChange,
  onNextPage,
  onPreviousPage,
  onSelectRun,
  loading,
}: RunHistoryProps) {
  const runs = data?.runs ?? [];
  const total = data?.total ?? 0;
  const offset = data?.offset ?? 0;

  const page = Math.floor(offset / PAGE_SIZE) + 1;
  const pages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  const emptyMessage = "No runs match these filters.";

  return (
    <section className="flex min-w-0 flex-col gap-4 xl:overflow-hidden">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
        <div className="flex items-baseline gap-2 sm:flex-col sm:gap-1">
          <Text variant="h3" as="h2">
            Run history
          </Text>
          <Text variant="caption" color="muted">
            {total} run{total === 1 ? "" : "s"}
            {loading ? " · refreshing…" : ""}
          </Text>
        </div>

        {/*
          Side by side rather than stacked even on the narrowest phone: two
          half-width selects still clear a comfortable tap target, and keeping
          them on one line leaves the runs themselves above the fold.
        */}
        <div className="flex gap-2 sm:max-w-72">
          <Select
            size="sm"
            label="Task"
            value={filters.taskId ?? ""}
            // brake-ui's Select does not propagate the DOM handler's parameter
            // type through its props, so annotate explicitly.
            onChange={(e: React.ChangeEvent<HTMLSelectElement>) => {
              onFilterChange({ taskId: e.target.value || undefined });
            }}
            fullWidth
          >
            <option value="">All tasks</option>
            {tasks.map((task) => (
              <option key={task.id} value={task.id}>
                {task.name}
              </option>
            ))}
          </Select>

          <Select
            size="sm"
            label="Status"
            value={filters.status ?? ""}
            onChange={(e: React.ChangeEvent<HTMLSelectElement>) => {
              const value = e.target.value;
              onFilterChange({
                // Options are generated from RUN_STATUSES, so anything non-empty
                // is a valid status.
                status: value ? (value as RunStatus) : undefined,
              });
            }}
            fullWidth
          >
            <option value="">Any status</option>
            {RUN_STATUSES.map((status) => (
              <option key={status} value={status}>
                {status}
              </option>
            ))}
          </Select>
        </div>
      </div>

      {/*
        Below md the six-column table becomes a stack of cards. On a phone the
        table is a horizontal scroll whose right half — result, duration — is
        exactly what the page is for.
      */}
      <div className="grid grid-cols-1 gap-2 md:hidden">
        {runs.length === 0 ? (
          <Card>
            <Text color="muted">{emptyMessage}</Text>
          </Card>
        ) : (
          runs.map((run) => (
            <RunListItem key={run.id} run={run} onSelect={onSelectRun} detailed />
          ))
        )}
      </div>

      <Card
        size="lg"
        className="hidden md:flex md:flex-col xl:min-h-0 xl:flex-1 xl:overflow-hidden"
      >
        <div className="overflow-auto xl:flex-1">
          <table className="w-full min-w-184 border-collapse text-left text-sm">
            <thead className="sticky top-0 z-10 border-b border-border bg-surface">
              <tr className="border-b border-border">
                {COLUMNS.map((heading) => (
                  <th
                    key={heading}
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
              {runs.length === 0 && (
                <tr>
                  <td colSpan={COLUMNS.length} className="px-3 py-8 text-center">
                    <Text color="muted">{emptyMessage}</Text>
                  </td>
                </tr>
              )}

              {runs.map((run) => (
                <tr
                  key={run.id}
                  onClick={() => {
                    onSelectRun(run);
                  }}
                  className="h-16 cursor-pointer border-b border-border transition-colors hover:bg-surface-hover"
                >
                  <td
                    className="px-3 whitespace-nowrap"
                    title={absoluteTime(run.started_at)}
                  >
                    {timeAgo(run.started_at)}
                  </td>
                  <td className="px-3 whitespace-nowrap">
                    {run.task_name ?? run.task_id}
                  </td>
                  <td className="px-3 whitespace-nowrap">
                    <RunBadges run={run} />
                  </td>
                  {/* w-full + max-w-0 is what makes truncate work in an
                      auto-layout table: the cell takes the leftover width but
                      reports no minimum, so the span clips instead of pushing. */}
                  <td className="w-full max-w-0 px-3">
                    <span className="block truncate font-mono text-xs text-text-secondary">
                      {runOutcome(run)}
                    </span>
                  </td>
                  <td className="px-3 whitespace-nowrap text-text-muted">
                    {duration(run.started_at, run.finished_at) ?? "—"}
                  </td>
                  <td className="px-3 whitespace-nowrap text-text-muted">
                    {run.trigger_source === "manual" ? "manual" : "—"}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </Card>

      {pages > 1 && (
        /*
          Spread to the edges on a phone, where both ends are within a thumb's
          reach; gathered into a group from sm up, because at full width the
          two buttons end up a screen apart with the page count marooned
          between them.
        */
        <div className="flex items-center justify-between gap-2 sm:justify-center sm:gap-6">
          <Button
            variant="ghost"
            icon={ChevronLeft}
            disabled={offset === 0}
            onClick={onPreviousPage}
          >
            Previous
          </Button>
          <Text variant="caption" color="muted" className="whitespace-nowrap">
            <span className="hidden sm:inline">Page </span>
            {page}
            <span className="hidden sm:inline"> of</span>
            <span className="sm:hidden"> /</span> {pages}
          </Text>
          <Button
            variant="ghost"
            icon={ChevronRight}
            iconPosition="right"
            disabled={offset + PAGE_SIZE >= total}
            onClick={onNextPage}
          >
            Next
          </Button>
        </div>
      )}
    </section>
  );
}
