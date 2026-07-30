import type {
  Channel,
  HealthResponse,
  RunsResponse,
  TaskWithStatus,
} from "../types/index.ts";
import {
  fetchTasks,
  createTask,
  updateTask,
  updateTaskEnabled,
  deleteTask,
  testTask,
  runTaskNow,
} from "../apis/tasks.api.ts";
import { fetchRuns, type FetchRunsOptions } from "../apis/runs.api.ts";
import { fetchHealth } from "../apis/health.api.ts";
import {
  fetchChannels,
  createChannel,
  updateChannel,
  deleteChannel,
  testChannel,
  testDraftChannel,
} from "../apis/channels.api.ts";

export interface MonitorSnapshot {
  tasks: TaskWithStatus[];
  runs: RunsResponse;
  health: HealthResponse;
  channels: Channel[];
}

/**
 * One consistent view of the system.
 *
 * Fetched in parallel and returned together so the UI updates in a single
 * render — separate awaits would let the task list and the run table disagree
 * about the same moment.
 */
export async function loadSnapshot(
  runFilters: FetchRunsOptions = {}
): Promise<MonitorSnapshot> {
  const [tasks, runs, health, channels] = await Promise.all([
    fetchTasks(),
    fetchRuns(runFilters),
    fetchHealth(),
    fetchChannels(),
  ]);

  return { tasks, runs, health, channels };
}

/** Re-exported so components depend on this layer rather than on `apis/`. */
export {
  createTask,
  updateTask,
  updateTaskEnabled,
  deleteTask,
  testTask,
  runTaskNow,
  createChannel,
  updateChannel,
  deleteChannel,
  testChannel,
  testDraftChannel,
};
