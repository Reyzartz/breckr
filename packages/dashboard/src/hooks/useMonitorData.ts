import { useCallback, useEffect, useRef, useState } from "react";
import type {
  CreateTaskRequest,
  HealthResponse,
  RunsResponse,
  TaskWithStatus,
  UpdateTaskRequest,
} from "@breckr/shared";
import {
  loadSnapshot,
  createTask,
  updateTask,
  updateTaskEnabled,
  deleteTask,
  runTaskNow,
} from "../services/monitor.service.ts";
import { toErrorMessage } from "../apis/client.ts";
import { config } from "../config/index.ts";
import { PAGE_SIZE } from "../constants/index.ts";
import type { RunFilters } from "./useRunFilters.ts";

export interface UseMonitorData {
  tasks: TaskWithStatus[];
  runs: RunsResponse | null;
  health: HealthResponse | null;
  error: string | null;
  loading: boolean;
  busyTaskId: string | null;
  refresh: () => Promise<void>;
  toggleTask: (id: string, enabled: boolean) => Promise<void>;
  runNow: (id: string) => Promise<void>;
  /**
   * Save a new or edited task.
   *
   * Unlike the others these rethrow: the form owns the error, because a
   * validation failure has to land on the offending field rather than in the
   * page-level banner, and the modal must stay open so the user can fix it.
   */
  addTask: (input: CreateTaskRequest) => Promise<void>;
  saveTask: (id: string, patch: UpdateTaskRequest) => Promise<void>;
  removeTask: (id: string) => Promise<void>;
}

/**
 * Owns all server state: the polling loop, the in-flight flags, and the last
 * error. Components stay presentational.
 */
export function useMonitorData(filters: RunFilters): UseMonitorData {
  const [tasks, setTasks] = useState<TaskWithStatus[]>([]);
  const [runs, setRuns] = useState<RunsResponse | null>(null);
  const [health, setHealth] = useState<HealthResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [busyTaskId, setBusyTaskId] = useState<string | null>(null);

  // The polling interval reads the current filters without being torn down and
  // rebuilt every time they change.
  const filtersRef = useRef(filters);
  filtersRef.current = filters;

  const refresh = useCallback(async () => {
    try {
      const { taskId, status, offset } = filtersRef.current;
      const snapshot = await loadSnapshot({
        taskId,
        status,
        offset,
        limit: PAGE_SIZE,
      });

      setTasks(snapshot.tasks);
      setRuns(snapshot.runs);
      setHealth(snapshot.health);
      setError(null);
    } catch (err) {
      setError(toErrorMessage(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void refresh();
    const id = setInterval(() => void refresh(), config.pollIntervalMs);
    return () => {
      clearInterval(id);
    };
  }, [refresh]);

  // Refetch immediately on a filter change rather than waiting for the poll.
  useEffect(() => {
    void refresh();
  }, [filters, refresh]);

  const toggleTask = useCallback(
    async (id: string, enabled: boolean) => {
      try {
        await updateTaskEnabled(id, enabled);
        await refresh();
      } catch (err) {
        setError(toErrorMessage(err));
      }
    },
    [refresh]
  );

  const runNow = useCallback(
    async (id: string) => {
      setBusyTaskId(id);
      try {
        await runTaskNow(id);
        await refresh();
      } catch (err) {
        setError(toErrorMessage(err));
      } finally {
        setBusyTaskId(null);
      }
    },
    [refresh]
  );

  const addTask = useCallback(
    async (input: CreateTaskRequest) => {
      await createTask(input);
      await refresh();
    },
    [refresh]
  );

  const saveTask = useCallback(
    async (id: string, patch: UpdateTaskRequest) => {
      await updateTask(id, patch);
      await refresh();
    },
    [refresh]
  );

  const removeTask = useCallback(
    async (id: string) => {
      setBusyTaskId(id);
      try {
        await deleteTask(id);
        await refresh();
      } catch (err) {
        setError(toErrorMessage(err));
      } finally {
        setBusyTaskId(null);
      }
    },
    [refresh]
  );

  return {
    tasks,
    runs,
    health,
    error,
    loading,
    busyTaskId,
    refresh,
    toggleTask,
    runNow,
    addTask,
    saveTask,
    removeTask,
  };
}
