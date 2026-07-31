import { useState } from "react";
import { createFileRoute } from "@tanstack/react-router";
import type { Run, RunStatus } from "../../types/index.ts";
import { RunHistory } from "../../components/RunHistory.tsx";
import { RunDetail } from "../../components/RunDetail.tsx";
import { useTasks } from "../../hooks/useTasks.ts";
import { useRuns, type RunFilters } from "../../hooks/useRuns.ts";
import { PAGE_SIZE } from "../../constants/index.ts";

function isRunStatus(value: unknown): value is RunStatus {
  return value === "running" || value === "success" || value === "failed";
}

/**
 * Filters and paging as URL search params.
 *
 * This is the payoff of moving run history onto its own route: a filtered,
 * paged view of the history is now a URL you can bookmark or send someone,
 * rather than state that resets the moment the tab reloads.
 */
export const Route = createFileRoute("/_authed/runs")({
  validateSearch: (search: Record<string, unknown>): RunFilters => ({
    taskId: typeof search.taskId === "string" ? search.taskId : undefined,
    status: isRunStatus(search.status) ? search.status : undefined,
    offset: Math.max(0, Number(search.offset) || 0),
  }),
  component: RunsPage,
});

function RunsPage() {
  const search = Route.useSearch();
  const navigate = Route.useNavigate();

  const { tasks } = useTasks();
  const { runs, isLoading } = useRuns(search);

  const [selectedRun, setSelectedRun] = useState<Run | null>(null);

  const setFilter = (patch: Partial<Omit<RunFilters, "offset">>) => {
    // Changing a filter resets to the first page — page 4 of the old filter
    // is meaningless.
    void navigate({ search: (prev) => ({ ...prev, ...patch, offset: 0 }) });
  };

  const nextPage = () => {
    void navigate({
      search: (prev) => ({ ...prev, offset: prev.offset + PAGE_SIZE }),
    });
  };

  const previousPage = () => {
    void navigate({
      search: (prev) => ({ ...prev, offset: Math.max(0, prev.offset - PAGE_SIZE) }),
    });
  };

  return (
    <div className="flex h-full flex-col">
      <RunHistory
        data={runs}
        tasks={tasks}
        filters={search}
        onFilterChange={setFilter}
        onNextPage={nextPage}
        onPreviousPage={previousPage}
        onSelectRun={setSelectedRun}
        loading={isLoading}
      />

      <RunDetail
        run={selectedRun}
        onClose={() => {
          setSelectedRun(null);
        }}
      />
    </div>
  );
}
