import type {
  Channel,
  CreateChannelRequest,
  TestChannelRequest,
  TestNotificationResponse,
  UpdateChannelRequest,
} from "../types/index.ts";
import { request } from "./client.ts";

export function fetchChannels(): Promise<Channel[]> {
  return request<Channel[]>("/channels");
}

export function createChannel(
  input: CreateChannelRequest
): Promise<Channel> {
  return request<Channel>("/channels", {
    method: "POST",
    body: JSON.stringify(input),
  });
}

/** Patch any subset of a channel; the server changes only what is present. */
export function updateChannel(
  id: string,
  patch: UpdateChannelRequest
): Promise<Channel> {
  return request<Channel>(`/channels/${encodeURIComponent(id)}`, {
    method: "PATCH",
    body: JSON.stringify(patch),
  });
}

/**
 * Task links go with it, so any task that alerted only here stops alerting.
 * Run history survives under the name the channel had.
 */
export function deleteChannel(id: string): Promise<void> {
  return request<void>(`/channels/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
}

/**
 * Send one real notification through a saved channel.
 *
 * Resolves even when nothing arrived: a rejection comes back as
 * `{ ok: false, status, detail }` rather than as a rejected promise, because
 * reporting why it failed *is* the point of the call.
 */
export function testChannel(id: string): Promise<TestNotificationResponse> {
  return request<TestNotificationResponse>(
    `/channels/${encodeURIComponent(id)}/test`,
    { method: "POST" }
  );
}

/**
 * Test a config that has not been saved yet, so a wrong token is caught while
 * the form is still open.
 *
 * Rejects on a validation failure — that error belongs on the offending field,
 * not in the outcome banner.
 */
export function testDraftChannel(
  input: TestChannelRequest
): Promise<TestNotificationResponse> {
  return request<TestNotificationResponse>("/channels/test", {
    method: "POST",
    body: JSON.stringify(input),
  });
}
