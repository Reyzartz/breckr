import { ApiClient } from "./base.ts";
import type {
  CreateTaskRequest,
  RunAcceptedResponse,
  TaskWithStatus,
  TestTaskRequest,
  TestTaskResponse,
  UpdateTaskRequest,
  UpdateTaskResponse,
} from "../../types/index.ts";

export class TaskService extends ApiClient {
  fetchTasks(): Promise<TaskWithStatus[]> {
    return this.get<TaskWithStatus[]>("/tasks");
  }

  createTask(input: CreateTaskRequest): Promise<TaskWithStatus> {
    return this.post<TaskWithStatus>("/tasks", input);
  }

  /** Patch any subset of a task; the server changes only what is present. */
  updateTask(id: string, patch: UpdateTaskRequest): Promise<UpdateTaskResponse> {
    return this.patch<UpdateTaskResponse>(`/tasks/${encodeURIComponent(id)}`, patch);
  }

  /** Run history goes with it -- the server cascades the delete. */
  deleteTask(id: string): Promise<void> {
    return this.delete(`/tasks/${encodeURIComponent(id)}`);
  }

  /**
   * Run a draft spec once without saving it.
   *
   * Resolves even when the run failed: a bad selector comes back as
   * `{ ok: false, error }` rather than as a rejected promise, because failing
   * is the expected case while you are still getting the selector right.
   */
  testTask(input: TestTaskRequest): Promise<TestTaskResponse> {
    return this.post<TestTaskResponse>("/tasks/test", input);
  }

  /**
   * Triggers immediately; works even while the task is disabled.
   *
   * Resolves as soon as the run has been *started*, not when it finishes --
   * the run row then arrives over the event socket as it appears and
   * resolves. There is no outcome to read here, and nothing to await for one.
   */
  runTaskNow(id: string): Promise<RunAcceptedResponse> {
    return this.post<RunAcceptedResponse>(`/tasks/${encodeURIComponent(id)}/run-now`);
  }
}

export const taskService = new TaskService();
