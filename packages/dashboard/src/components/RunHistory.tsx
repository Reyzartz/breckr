import { Badge, Button, Card, Select, Text } from "brake-ui";
import { ChevronLeft, ChevronRight } from "lucide-react";
import type {
  Run,
  RunStatus,
  RunsResponse,
  TaskWithStatus,
} from "@breckr/shared";
import { StatusBadge } from "./StatusBadge.tsx";
import { PAGE_SIZE, RUN_STATUSES } from "../constants/index.ts";
import type { RunFilters } from "../hooks/useRunFilters.ts";
import {
  absoluteTime,
  duration,
  firstLine,
  summarize,
  timeAgo,
} from "../utils/format.ts";

const COLUMNS = ["When", "Task", "Status", "Result", "Took", ""] as const;

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

  return (
    <section className="flex flex-col gap-4 items-stretch overflow-hidden">
      <div className="flex gap-2">
        <div className="flex-1 flex flex-col gap-2">
          <Text variant="h4" as="h2">
            Run history
          </Text>
          <Text variant="caption" color="muted">
            {total} run{total === 1 ? "" : "s"}
          </Text>
        </div>

        <div className="max-w-72 flex gap-2">
          <Select
            size="sm"
            label="Task"
            value={filters.taskId ?? ""}
            // brake-ui's Select does not propagate the DOM handler's parameter
            // type through its props, so annotate explicitly.
            onChange={(e: React.ChangeEvent<HTMLSelectElement>) => {
              onFilterChange({ taskId: e.target.value || undefined });
            }}
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

      <Card size="lg" className="flex-1 overflow-hidden flex flex-col">
        <div className="flex flex-wrap items-end justify-between gap-3">
          <Text variant="caption" color="muted">
            {loading ? " · refreshing…" : ""}
          </Text>
        </div>

        <div className="overflow-auto flex-1">
          <table className="w-full min-w-184 border-collapse text-left text-sm">
            <thead className="sticky top-0 bg-surface z-10 border-b border-border">
              <tr className="border-b border-border">
                {COLUMNS.map((heading, i) => (
                  <th
                    key={heading || `col-${String(i)}`}
                    className="px-3 py-2 font-medium whitespace-nowrap text-text-muted"
                  >
                    {heading}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {runs.length === 0 && (
                <tr>
                  <td
                    colSpan={COLUMNS.length}
                    className="px-3 py-8 text-center"
                  >
                    <Text color="muted">No runs match these filters.</Text>
                  </td>
                </tr>
              )}

              {runs.map((run) => (
                <tr
                  key={run.id}
                  onClick={() => {
                    onSelectRun(run);
                  }}
                  className="cursor-pointer border-b border-border transition-colors hover:bg-surface-hover h-16"
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
                    <div className="flex items-center gap-1.5">
                      <StatusBadge status={run.status} />
                      {run.notified && (
                        <Badge variant="warning">notified</Badge>
                      )}
                      {run.condition_met && !run.notified && (
                        <Badge variant="info">met</Badge>
                      )}
                    </div>
                  </td>
                  <td className="max-w-md px-3">
                    <span className="block truncate font-mono text-xs text-text-secondary">
                      {run.status === "failed"
                        ? firstLine(run.error)
                        : summarize(run.result_summary)}
                    </span>
                  </td>
                  <td className="px-3 whitespace-nowrap text-text-muted">
                    {duration(run.started_at, run.finished_at) ?? "—"}
                  </td>
                  <td className="px-3 whitespace-nowrap text-text-muted">
                    {run.trigger_source === "manual" ? "manual" : ""}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>

        {pages > 1 && (
          <div className="mt-4 flex items-center justify-between">
            <Button
              size="sm"
              variant="ghost"
              icon={ChevronLeft}
              disabled={offset === 0}
              onClick={onPreviousPage}
            >
              Previous
            </Button>
            <Text variant="caption" color="muted">
              Page {page} of {pages}
            </Text>
            <Button
              size="sm"
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
      </Card>
    </section>
  );
}
