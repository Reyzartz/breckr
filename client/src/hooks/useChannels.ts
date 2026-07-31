import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type {
  CreateChannelRequest,
  TestChannelRequest,
  TestNotificationResponse,
  UpdateChannelRequest,
} from "../types/index.ts";
import { channelService, toErrorMessage } from "../services/api/index.ts";
import { QueryKeys } from "../constants/queryKeys.ts";
import { config } from "../config/index.ts";

/** Synthesizes an outcome from a rejected test, so a network failure renders
 * in the same place a reported delivery failure does. Landing here means the
 * request itself never got an answer, which is different from the server
 * reporting a rejected delivery -- but both read as "did not arrive" to the
 * user, and the detail line says which one happened. */
function outcomeFromError(err: unknown): TestNotificationResponse {
  return {
    ok: false,
    status: "error",
    detail: toErrorMessage(err),
    message: "",
    attemptedAt: new Date().toISOString(),
  };
}

/**
 * Delivery channels: the list, every mutation, and both flavors of "send one
 * real notification to prove it works" -- against a saved channel, and
 * against a config that has not been saved yet.
 *
 * `createChannel` and `updateChannel` rethrow -- the form owns that error, the
 * same reason `useTasks`'s create/update do. `deleteChannel` does not: it is
 * fired from the list, so its failure surfaces through `error` instead.
 */
export function useChannels() {
  const queryClient = useQueryClient();

  const channelsQuery = useQuery({
    queryKey: QueryKeys.channels,
    queryFn: () => channelService.fetchChannels(),
    refetchInterval: config.pollIntervalMs,
  });

  const invalidateChannels = () =>
    queryClient.invalidateQueries({ queryKey: QueryKeys.channels });
  // A task's channel_ids can be affected by a channel going away.
  const invalidateTasks = () => queryClient.invalidateQueries({ queryKey: QueryKeys.tasks });

  const createMutation = useMutation({
    mutationFn: (input: CreateChannelRequest) => channelService.createChannel(input),
    onSuccess: invalidateChannels,
  });

  const updateMutation = useMutation({
    mutationFn: ({ id, patch }: { id: string; patch: UpdateChannelRequest }) =>
      channelService.updateChannel(id, patch),
    onSuccess: invalidateChannels,
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) => channelService.deleteChannel(id),
    onSuccess: () => {
      invalidateChannels();
      invalidateTasks();
    },
  });

  // Tests never reject to the caller -- the outcome, delivered or not, is the
  // whole point of the call -- so the result is read back off the mutation's
  // own state rather than from a return value.
  const testMutation = useMutation({
    mutationFn: (id: string) => channelService.testChannel(id),
  });

  const testDraftMutation = useMutation({
    mutationFn: (input: TestChannelRequest) => channelService.testDraftChannel(input),
  });

  const testResult: TestNotificationResponse | null = testMutation.isSuccess
    ? testMutation.data
    : testMutation.isError
      ? outcomeFromError(testMutation.error)
      : null;

  const listError = deleteMutation.error ?? channelsQuery.error;

  return {
    channels: channelsQuery.data ?? [],
    isLoading: channelsQuery.isLoading,
    error: listError ? toErrorMessage(listError) : null,

    // Rethrow: the form owns the error.
    createChannel: (input: CreateChannelRequest) => createMutation.mutateAsync(input),
    updateChannel: (id: string, patch: UpdateChannelRequest) =>
      updateMutation.mutateAsync({ id, patch }),
    // Rejects on validation only (a 400 naming a field); a config that tests
    // clean but does not deliver resolves normally as `{ ok: false }`.
    testDraftChannel: (input: TestChannelRequest) => testDraftMutation.mutateAsync(input),

    // Swallowed: fired from the list, so the failure surfaces through `error`.
    deleteChannel: async (id: string): Promise<void> => {
      try {
        await deleteMutation.mutateAsync(id);
      } catch {
        // Reported through `error` above.
      }
    },

    testChannel: (id: string) => {
      testMutation.mutate(id);
    },
    /** Id of the channel currently being tested, or null. */
    channelBeingTested: testMutation.isPending ? (testMutation.variables ?? null) : null,
    /** Outcome of the last channel test, from either flavor above. */
    testResult,
    dismissTestResult: () => {
      testMutation.reset();
    },
  };
}
