import { useCallback, useEffect, useRef, useState } from "react";
import type {
  Channel,
  CreateChannelRequest,
  CreateTaskRequest,
  HealthResponse,
  MonitorResource,
  RunsResponse,
  TaskWithStatus,
  TestNotificationResponse,
  UpdateChannelRequest,
  UpdateTaskRequest,
} from "../types/index.ts";
import {
  ALL_RESOURCES,
  loadSnapshot,
  createTask,
  updateTask,
  updateTaskEnabled,
  deleteTask,
  runTaskNow,
  createChannel,
  updateChannel,
  deleteChannel,
  testChannel,
} from "../services/monitor.service.ts";
import { toErrorMessage } from "../apis/client.ts";
import { PAGE_SIZE, RUN_PENDING_TIMEOUT_MS } from "../constants/index.ts";
import type { RunFilters } from "./useRunFilters.ts";
import { useMonitorEvents, type ConnectionState } from "./useMonitorEvents.ts";

export interface UseMonitorData {
  tasks: TaskWithStatus[];
  runs: RunsResponse | null;
  health: HealthResponse | null;
  channels: Channel[];
  error: string | null;
  loading: boolean;
  /** State of the live connection, for the header's indicator. */
  connection: ConnectionState;
  /**
   * Whether a task is mid-something and its controls should be disabled.
   *
   * A function rather than an id because "running" is server state now — see
   * runNow — so it cannot be answered without the task in hand.
   */
  isTaskBusy: (task: TaskWithStatus) => boolean;
  /** Id of the channel currently being tested, or null. */
  testingChannelId: string | null;
  /** Outcome of the last test send; null until one has been attempted. */
  notificationTest: TestNotificationResponse | null;
  refresh: (resources?: readonly MonitorResource[]) => Promise<void>;
  toggleTask: (id: string, enabled: boolean) => Promise<void>;
  runNow: (id: string) => Promise<void>;
  /**
   * Send one real notification through a channel to prove it works.
   *
   * Never sets the page-level error: a rejected delivery is the answer, not a
   * fault in the dashboard, and it belongs beside the button that asked.
   */
  runChannelTest: (id: string) => Promise<void>;
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
  /** Rethrow for the same reason addTask does — the form owns field errors. */
  addChannel: (input: CreateChannelRequest) => Promise<void>;
  saveChannel: (id: string, patch: UpdateChannelRequest) => Promise<void>;
  removeChannel: (id: string) => Promise<void>;
}

/** Newest refetch to have written each resource, so a slow one cannot win. */
type AppliedSeq = Record<MonitorResource, number>;

const NO_PENDING_RUNS: ReadonlySet<string> = new Set();

/**
 * Owns all server state: the live connection, the in-flight flags, and the last
 * error. Components stay presentational.
 *
 * There is no polling. The server pushes what changed over /api/events and this
 * refetches only that, which means a run appears the moment it starts rather
 * than whenever a timer next happened to fire.
 */
export function useMonitorData(filters: RunFilters): UseMonitorData {
  const [tasks, setTasks] = useState<TaskWithStatus[]>([]);
  const [runs, setRuns] = useState<RunsResponse | null>(null);
  const [health, setHealth] = useState<HealthResponse | null>(null);
  const [channels, setChannels] = useState<Channel[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [busyTaskId, setBusyTaskId] = useState<string | null>(null);
  const [testingChannelId, setTestingChannelId] = useState<string | null>(null);
  const [notificationTest, setNotificationTest] =
    useState<TestNotificationResponse | null>(null);

  // Read by refresh without making it depend on the current filters, so the
  // socket's callback never has to be rebuilt.
  const filtersRef = useRef(filters);
  filtersRef.current = filters;

  // Events arrive whenever the server has news, so two refetches overlapping is
  // ordinary rather than exceptional — and responses can land out of order. Each
  // call is stamped, and a response only writes a resource if no later call has
  // already written it.
  const seqRef = useRef(0);
  const appliedRef = useRef<AppliedSeq>({
    tasks: 0,
    runs: 0,
    health: 0,
    channels: 0,
  });

  const refresh = useCallback(
    async (resources: readonly MonitorResource[] = ALL_RESOURCES) => {
      const seq = ++seqRef.current;

      const claim = (resource: MonitorResource) => {
        if (seq < appliedRef.current[resource]) return false;
        appliedRef.current[resource] = seq;
        return true;
      };

      try {
        const { taskId, status, offset } = filtersRef.current;
        const snapshot = await loadSnapshot(
          { taskId, status, offset, limit: PAGE_SIZE },
          resources
        );

        if (snapshot.tasks !== undefined && claim("tasks")) {
          setTasks(snapshot.tasks);
        }
        if (snapshot.runs !== undefined && claim("runs")) {
          setRuns(snapshot.runs);
        }
        if (snapshot.health !== undefined && claim("health")) {
          setHealth(snapshot.health);
        }
        if (snapshot.channels !== undefined && claim("channels")) {
          setChannels(snapshot.channels);
        }

        // Only the newest attempt gets to say the page is healthy again --
        // otherwise a stale success clears an error the current state still has.
        if (seq === seqRef.current) setError(null);
      } catch (err) {
        if (seq === seqRef.current) setError(toErrorMessage(err));
      } finally {
        setLoading(false);
      }
    },
    []
  );

  // A change event names what moved; a fresh connection passes nothing, because
  // a socket that has just come up cannot know what it missed and refetches
  // everything.
  const connection = useMonitorEvents({
    onChange: useCallback(
      (resources: readonly MonitorResource[] | undefined) => {
        void refresh(resources);
      },
      [refresh]
    ),
  });

  // Filtering and pagination are server-side, so a filter change is a runs
  // refetch and nothing else. This also runs on mount, which paints the table
  // without waiting for the socket; everything else arrives with the resync the
  // connection triggers.
  useEffect(() => {
    void refresh(["runs"]);
  }, [filters, refresh]);

  // --- "Run now" ------------------------------------------------------------
  //
  // run-now no longer waits for the run, so the button cannot be driven by the
  // request. It is driven by the run row instead -- last_run.status === running
  // -- with this set covering only the gap between the request being accepted
  // and that row arriving.
  const [pendingRuns, setPendingRuns] =
    useState<ReadonlySet<string>>(NO_PENDING_RUNS);
  const pendingTimers = useRef(new Map<string, number>());

  const clearPending = useCallback((id: string) => {
    const timer = pendingTimers.current.get(id);
    if (timer !== undefined) {
      window.clearTimeout(timer);
      pendingTimers.current.delete(id);
    }

    setPendingRuns((current) => {
      if (!current.has(id)) return current;
      const next = new Set(current);
      next.delete(id);
      return next;
    });
  }, []);

  const runNow = useCallback(
    async (id: string) => {
      setPendingRuns((current) => new Set(current).add(id));

      window.clearTimeout(pendingTimers.current.get(id));
      pendingTimers.current.set(
        id,
        window.setTimeout(() => {
          clearPending(id);
        }, RUN_PENDING_TIMEOUT_MS)
      );

      try {
        await runTaskNow(id);
      } catch (err) {
        // Nothing was started, so there is no run row coming to take over.
        clearPending(id);
        setError(toErrorMessage(err));
      }
    },
    [clearPending]
  );

  // The moment the server reports the run, server truth takes over and the
  // optimistic entry is no longer needed.
  useEffect(() => {
    for (const task of tasks) {
      if (pendingRuns.has(task.id) && task.last_run?.status === "running") {
        clearPending(task.id);
      }
    }
  }, [tasks, pendingRuns, clearPending]);

  useEffect(() => {
    const timers = pendingTimers.current;
    return () => {
      timers.forEach((timer) => {
        window.clearTimeout(timer);
      });
      timers.clear();
    };
  }, []);

  const isTaskBusy = useCallback(
    (task: TaskWithStatus) =>
      busyTaskId === task.id ||
      pendingRuns.has(task.id) ||
      task.last_run?.status === "running",
    [busyTaskId, pendingRuns]
  );

  // --- Mutations ------------------------------------------------------------
  //
  // Each still refetches what it touched. The server announces these too, so
  // other tabs keep up, but the tab that clicked should not have to wait on the
  // socket -- or depend on it being connected at all -- to see its own change.

  const toggleTask = useCallback(
    async (id: string, enabled: boolean) => {
      try {
        await updateTaskEnabled(id, enabled);
        await refresh(["tasks"]);
      } catch (err) {
        setError(toErrorMessage(err));
      }
    },
    [refresh]
  );

  const runChannelTest = useCallback(async (id: string) => {
    setTestingChannelId(id);
    setNotificationTest(null);
    try {
      setNotificationTest(await testChannel(id));
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
      setTestingChannelId(null);
    }
  }, []);

  const dismissNotificationTest = useCallback(() => {
    setNotificationTest(null);
  }, []);

  const addTask = useCallback(
    async (input: CreateTaskRequest) => {
      await createTask(input);
      await refresh(["tasks"]);
    },
    [refresh]
  );

  const saveTask = useCallback(
    async (id: string, patch: UpdateTaskRequest) => {
      await updateTask(id, patch);
      await refresh(["tasks"]);
    },
    [refresh]
  );

  const removeTask = useCallback(
    async (id: string) => {
      setBusyTaskId(id);
      try {
        await deleteTask(id);
        // Runs too: the delete cascaded through this task's history.
        await refresh(["tasks", "runs"]);
      } catch (err) {
        setError(toErrorMessage(err));
      } finally {
        setBusyTaskId(null);
      }
    },
    [refresh]
  );

  const addChannel = useCallback(
    async (input: CreateChannelRequest) => {
      await createChannel(input);
      await refresh(["channels", "health"]);
    },
    [refresh]
  );

  const saveChannel = useCallback(
    async (id: string, patch: UpdateChannelRequest) => {
      await updateChannel(id, patch);
      await refresh(["channels", "health"]);
    },
    [refresh]
  );

  const removeChannel = useCallback(
    async (id: string) => {
      try {
        await deleteChannel(id);
        // Tasks too: the deleted channel's task links went with it.
        await refresh(["channels", "health", "tasks"]);
      } catch (err) {
        setError(toErrorMessage(err));
      }
    },
    [refresh]
  );

  return {
    tasks,
    runs,
    health,
    channels,
    error,
    loading,
    connection,
    isTaskBusy,
    testingChannelId,
    notificationTest,
    refresh,
    toggleTask,
    runNow,
    runChannelTest,
    dismissNotificationTest,
    addTask,
    saveTask,
    removeTask,
    addChannel,
    saveChannel,
    removeChannel,
  };
}
