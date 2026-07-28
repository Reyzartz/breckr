import { useCallback, useState } from "react";
import type { RunStatus } from "../types/index.ts";
import { PAGE_SIZE } from "../constants/index.ts";

export interface RunFilters {
  taskId?: string | undefined;
  status?: RunStatus | undefined;
  offset: number;
}

export interface UseRunFilters {
  filters: RunFilters;
  /** Changing a filter resets to the first page — page 4 of the old filter is meaningless. */
  setFilter: (patch: Partial<Omit<RunFilters, "offset">>) => void;
  nextPage: () => void;
  previousPage: () => void;
}

export function useRunFilters(): UseRunFilters {
  const [filters, setFilters] = useState<RunFilters>({ offset: 0 });

  const setFilter = useCallback(
    (patch: Partial<Omit<RunFilters, "offset">>) => {
      setFilters((current) => ({ ...current, ...patch, offset: 0 }));
    },
    []
  );

  const nextPage = useCallback(() => {
    setFilters((current) => ({ ...current, offset: current.offset + PAGE_SIZE }));
  }, []);

  const previousPage = useCallback(() => {
    setFilters((current) => ({
      ...current,
      offset: Math.max(0, current.offset - PAGE_SIZE),
    }));
  }, []);

  return { filters, setFilter, nextPage, previousPage };
}
