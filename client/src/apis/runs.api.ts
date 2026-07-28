import type { Run, RunsResponse, RunStatus } from "../types/index.ts";
import { request } from "./client.ts";
import { PAGE_SIZE } from "../constants/index.ts";

export interface FetchRunsOptions {
  taskId?: string | undefined;
  status?: RunStatus | undefined;
  limit?: number;
  offset?: number;
}

export function fetchRuns({
  taskId,
  status,
  limit = PAGE_SIZE,
  offset = 0,
}: FetchRunsOptions = {}): Promise<RunsResponse> {
  const params = new URLSearchParams();
  if (taskId) params.set("task_id", taskId);
  if (status) params.set("status", status);
  params.set("limit", String(limit));
  params.set("offset", String(offset));

  return request<RunsResponse>(`/runs?${params.toString()}`);
}

export function fetchRun(id: number): Promise<Run> {
  return request<Run>(`/runs/${String(id)}`);
}
