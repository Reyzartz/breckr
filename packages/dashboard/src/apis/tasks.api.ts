import type {
  CreateTaskRequest,
  TasksResponse,
  TaskWithStatus,
  TestTaskRequest,
  TestTaskResponse,
  UpdateTaskRequest,
  UpdateTaskResponse,
  RunOutcome,
} from "@breckr/shared";
import { request } from "./client.ts";

export function fetchTasks(): Promise<TasksResponse> {
  return request<TasksResponse>("/tasks");
}

export function createTask(input: CreateTaskRequest): Promise<TaskWithStatus> {
  return request<TaskWithStatus>("/tasks", {
    method: "POST",
    body: JSON.stringify(input),
  });
}

/** Patch any subset of a task; the server changes only what is present. */
export function updateTask(
  id: string,
  patch: UpdateTaskRequest
): Promise<UpdateTaskResponse> {
  return request<UpdateTaskResponse>(`/tasks/${encodeURIComponent(id)}`, {
    method: "PATCH",
    body: JSON.stringify(patch),
  });
}

export function updateTaskEnabled(
  id: string,
  enabled: boolean
): Promise<UpdateTaskResponse> {
  return updateTask(id, { enabled });
}

/** Run history goes with it — the server cascades the delete. */
export function deleteTask(id: string): Promise<void> {
  return request<void>(`/tasks/${encodeURIComponent(id)}`, { method: "DELETE" });
}

/**
 * Run a draft spec once without saving it.
 *
 * Resolves even when the run failed: a bad selector comes back as
 * `{ ok: false, error }` rather than as a rejected promise, because failing is
 * the expected case while you are still getting the selector right.
 */
export function testTask(input: TestTaskRequest): Promise<TestTaskResponse> {
  return request<TestTaskResponse>("/tasks/test", {
    method: "POST",
    body: JSON.stringify(input),
  });
}

/** Triggers immediately; works even while the task is disabled. */
export function runTaskNow(id: string): Promise<RunOutcome> {
  return request<RunOutcome>(`/tasks/${encodeURIComponent(id)}/run-now`, {
    method: "POST",
  });
}
