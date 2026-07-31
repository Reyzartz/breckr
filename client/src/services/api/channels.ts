import { ApiClient } from "./base.ts";
import type {
  Channel,
  CreateChannelRequest,
  TestChannelRequest,
  TestNotificationResponse,
  UpdateChannelRequest,
} from "../../types/index.ts";

export class ChannelService extends ApiClient {
  fetchChannels(): Promise<Channel[]> {
    return this.get<Channel[]>("/channels");
  }

  createChannel(input: CreateChannelRequest): Promise<Channel> {
    return this.post<Channel>("/channels", input);
  }

  /** Patch any subset of a channel; the server changes only what is present. */
  updateChannel(id: string, patch: UpdateChannelRequest): Promise<Channel> {
    return this.patch<Channel>(`/channels/${encodeURIComponent(id)}`, patch);
  }

  /**
   * Task links go with it, so any task that alerted only here stops alerting.
   * Run history survives under the name the channel had.
   */
  deleteChannel(id: string): Promise<void> {
    return this.delete(`/channels/${encodeURIComponent(id)}`);
  }

  /**
   * Send one real notification through a saved channel.
   *
   * Resolves even when nothing arrived: a rejection comes back as
   * `{ ok: false, status, detail }` rather than as a rejected promise, because
   * reporting why it failed *is* the point of the call.
   */
  testChannel(id: string): Promise<TestNotificationResponse> {
    return this.post<TestNotificationResponse>(`/channels/${encodeURIComponent(id)}/test`);
  }

  /**
   * Test a config that has not been saved yet, so a wrong token is caught
   * while the form is still open.
   *
   * Rejects on a validation failure -- that error belongs on the offending
   * field, not in the outcome banner.
   */
  testDraftChannel(input: TestChannelRequest): Promise<TestNotificationResponse> {
    return this.post<TestNotificationResponse>("/channels/test", input);
  }
}

export const channelService = new ChannelService();
