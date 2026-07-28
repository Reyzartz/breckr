import type { HealthResponse } from "../types/index.ts";
import { request } from "./client.ts";

export function fetchHealth(): Promise<HealthResponse> {
  return request<HealthResponse>("/health");
}
