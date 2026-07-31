import type {
  Channel,
  HealthResponse,
  MonitorResource,
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

export const ALL_RESOURCES: readonly MonitorResource[] = [
  "tasks",
  "runs",
  "health",
  "channels",
];

/**
 * One consistent view of the requested resources.
 *
 * Fetched in parallel and returned together so the UI updates in a single
 * render — separate awaits would let the task list and the run table disagree
 * about the same moment.
 *
 * Scoped rather than always-everything because a change event names what moved,
 * and refetching the rest would be wasted work. `/api/health` in particular
 * probes the browser over CDP and takes the same global mutex task runs queue
 * behind, so a finished run must not drag it along.
 *
 * Resources not asked for are absent from the result, which is not the same as
 * being empty — the caller keeps whatever it already had.
 */
export async function loadSnapshot(
  runFilters: FetchRunsOptions = {},
  resources: readonly MonitorResource[] = ALL_RESOURCES
): Promise<Partial<MonitorSnapshot>> {
  const wanted = new Set(resources);

  const [tasks, runs, health, channels] = await Promise.all([
    wanted.has("tasks") ? fetchTasks() : undefined,
    wanted.has("runs") ? fetchRuns(runFilters) : undefined,
    wanted.has("health") ? fetchHealth() : undefined,
    wanted.has("channels") ? fetchChannels() : undefined,
  ]);

  const snapshot: Partial<MonitorSnapshot> = {};
  if (tasks !== undefined) snapshot.tasks = tasks;
  if (runs !== undefined) snapshot.runs = runs;
  if (health !== undefined) snapshot.health = health;
  if (channels !== undefined) snapshot.channels = channels;

  return snapshot;
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
