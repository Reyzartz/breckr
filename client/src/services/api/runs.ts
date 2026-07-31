import { ApiClient } from "./base.ts";
import type { Run, RunsResponse, RunStatus } from "../../types/index.ts";
import { PAGE_SIZE } from "../../constants/index.ts";

export interface FetchRunsParams {
  taskId?: string | undefined;
  status?: RunStatus | undefined;
  limit?: number;
  offset?: number;
}

export class RunService extends ApiClient {
  fetchRuns({
    taskId,
    status,
    limit = PAGE_SIZE,
    offset = 0,
  }: FetchRunsParams = {}): Promise<RunsResponse> {
    const params = new URLSearchParams();
    if (taskId) params.set("task_id", taskId);
    if (status) params.set("status", status);
    params.set("limit", String(limit));
    params.set("offset", String(offset));

    return this.get<RunsResponse>(`/runs?${params.toString()}`);
  }

  fetchRun(id: number): Promise<Run> {
    return this.get<Run>(`/runs/${String(id)}`);
  }
}

export const runService = new RunService();
