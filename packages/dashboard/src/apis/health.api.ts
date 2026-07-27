import type { HealthResponse } from "@breckr/shared";
import { request } from "./client.ts";

export function fetchHealth(): Promise<HealthResponse> {
  return request<HealthResponse>("/health");
}
