import { ApiClient } from "./base.ts";
import type { HealthResponse } from "../../types/index.ts";

export class HealthService extends ApiClient {
  fetchHealth(): Promise<HealthResponse> {
    return this.get<HealthResponse>("/health");
  }
}

export const healthService = new HealthService();
