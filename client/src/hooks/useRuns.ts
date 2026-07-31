import { keepPreviousData, useQuery } from "@tanstack/react-query";
import type { RunStatus } from "../types/index.ts";
import { runService, toErrorMessage } from "../services/api/index.ts";
import { QueryKeys } from "../constants/queryKeys.ts";
import { config } from "../config/index.ts";
import { PAGE_SIZE } from "../constants/index.ts";

export interface RunFilters {
  taskId?: string | undefined;
  status?: RunStatus | undefined;
  offset: number;
  /** Defaults to PAGE_SIZE; RecentRuns asks for a smaller page. */
  limit?: number;
}

/**
 * Paginated run history for one set of filters.
 *
 * `placeholderData: keepPreviousData` is what keeps the table showing the last
 * page's rows while a filter or page change is in flight, rather than
 * flashing to empty -- the old polling loop got this for free by never
 * clearing its state between fetches; a query keyed on the filters would
 * otherwise blank out on every change since it is, as far as the cache is
 * concerned, a different query.
 */
export function useRuns(filters: RunFilters) {
  const query = useQuery({
    queryKey: [...QueryKeys.runs, filters],
    queryFn: () => runService.fetchRuns({ ...filters, limit: filters.limit ?? PAGE_SIZE }),
    refetchInterval: config.pollIntervalMs,
    placeholderData: keepPreviousData,
  });

  return {
    runs: query.data ?? null,
    isLoading: query.isLoading,
    isFetching: query.isFetching,
    error: query.error ? toErrorMessage(query.error) : null,
  };
}

/**
 * One run's full detail, including the per-channel notification attempts the
 * list view does not carry.
 *
 * Only fetched on demand -- `id` is null while no run is selected -- so
 * opening the detail modal is what triggers the extra query, not every row in
 * the table.
 */
export function useRun(id: number | null) {
  const query = useQuery({
    queryKey: [...QueryKeys.runs, "detail", id],
    queryFn: () => runService.fetchRun(id as number),
    enabled: id !== null,
  });

  return { run: query.data ?? null, isLoading: query.isLoading };
}
