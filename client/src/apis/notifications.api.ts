import type { TestNotificationResponse } from "../types/index.ts";
import { request } from "./client.ts";

/**
 * Send one real notification, right now.
 *
 * Resolves even when nothing arrived: an unconfigured transport or a rejection
 * comes back as `{ ok: false, status, detail }` rather than as a rejected
 * promise, because reporting why it failed *is* the point of the call.
 */
export function sendTestNotification(): Promise<TestNotificationResponse> {
  return request<TestNotificationResponse>("/notifications/test", {
    method: "POST",
  });
}
