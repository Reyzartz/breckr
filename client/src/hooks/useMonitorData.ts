import { useCallback, useEffect, useRef, useState } from "react";
import type {
  CreateTaskRequest,
  HealthResponse,
  RunsResponse,
  TaskWithStatus,
  TestNotificationResponse,
  UpdateTaskRequest,
} from "../types/index.ts";
import {
  loadSnapshot,
  createTask,
  updateTask,
  updateTaskEnabled,
  deleteTask,
  runTaskNow,
  sendTestNotification,
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
  /** In-flight flag for the test notification. */
  testingNotification: boolean;
  /** Outcome of the last test send; null until one has been attempted. */
  notificationTest: TestNotificationResponse | null;
  refresh: () => Promise<void>;
  toggleTask: (id: string, enabled: boolean) => Promise<void>;
  runNow: (id: string) => Promise<void>;
  /**
   * Send one real notification to prove the transport works.
   *
   * Never sets the page-level error: a rejected delivery is the answer, not a
   * fault in the dashboard, and it belongs beside the button that asked.
   */
  testNotification: () => Promise<void>;
  dismissNotificationTest: () => void;
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
  const [testingNotification, setTestingNotification] = useState(false);
  const [notificationTest, setNotificationTest] =
    useState<TestNotificationResponse | null>(null);

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

  const testNotification = useCallback(async () => {
    setTestingNotification(true);
    setNotificationTest(null);
    try {
      setNotificationTest(await sendTestNotification());
    } catch (err) {
      // The server reports a rejected delivery in the response body, so landing
      // here means the request itself never got an answer. Shaped like an
      // outcome so it renders in the same place as one.
      setNotificationTest({
        ok: false,
        status: "error",
        detail: toErrorMessage(err),
        message: "",
        attemptedAt: new Date().toISOString(),
      });
    } finally {
      setTestingNotification(false);
    }
  }, []);

  const dismissNotificationTest = useCallback(() => {
    setNotificationTest(null);
  }, []);

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
    testingNotification,
    notificationTest,
    refresh,
    toggleTask,
    runNow,
    testNotification,
    dismissNotificationTest,
    addTask,
    saveTask,
    removeTask,
  };
}
