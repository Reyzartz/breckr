import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type {
  CreateTaskRequest,
  TestTaskRequest,
  UpdateTaskRequest,
} from "../types/index.ts";
import { taskService, toErrorMessage } from "../services/api/index.ts";
import { QueryKeys } from "../constants/queryKeys.ts";
import { config } from "../config/index.ts";

/**
 * Tasks: the list, and every mutation the dashboard performs on one.
 *
 * `createTask` and `updateTask` rethrow -- the form owns that error, because a
 * validation failure has to land on the offending field rather than in a
 * page-level banner, and the modal must stay open so the user can fix it.
 * `toggleTaskEnabled`, `runTaskNow` and `deleteTask` do not: they are fired
 * from the list itself, where there is no field to blame, so their failure
 * surfaces as `error` instead.
 */
export function useTasks() {
  const queryClient = useQueryClient();

  const tasksQuery = useQuery({
    queryKey: QueryKeys.tasks,
    queryFn: () => taskService.fetchTasks(),
    refetchInterval: config.pollIntervalMs,
  });

  const invalidateTasks = () => queryClient.invalidateQueries({ queryKey: QueryKeys.tasks });
  // A run just happened or a task's history was deleted with it -- the run
  // table needs to catch up too, not just the task list.
  const invalidateRuns = () => queryClient.invalidateQueries({ queryKey: QueryKeys.runs });

  const createMutation = useMutation({
    mutationFn: (input: CreateTaskRequest) => taskService.createTask(input),
    onSuccess: invalidateTasks,
  });

  const updateMutation = useMutation({
    mutationFn: ({ id, patch }: { id: string; patch: UpdateTaskRequest }) =>
      taskService.updateTask(id, patch),
    onSuccess: invalidateTasks,
  });

  const toggleMutation = useMutation({
    mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) =>
      taskService.updateTask(id, { enabled }),
    onSuccess: invalidateTasks,
  });

  const runNowMutation = useMutation({
    mutationFn: (id: string) => taskService.runTaskNow(id),
    onSuccess: () => {
      invalidateTasks();
      invalidateRuns();
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) => taskService.deleteTask(id),
    onSuccess: () => {
      invalidateTasks();
      invalidateRuns();
    },
  });

  const testMutation = useMutation({
    mutationFn: (input: TestTaskRequest) => taskService.testTask(input),
  });

  /** A task with an in-flight "run now" or delete -- what TaskCard disables. */
  function isTaskBusy(id: string): boolean {
    return (
      (runNowMutation.isPending && runNowMutation.variables === id) ||
      (deleteMutation.isPending && deleteMutation.variables === id)
    );
  }

  const actionError =
    toggleMutation.error ?? runNowMutation.error ?? deleteMutation.error ?? tasksQuery.error;

  return {
    tasks: tasksQuery.data ?? [],
    isLoading: tasksQuery.isLoading,
    error: actionError ? toErrorMessage(actionError) : null,
    isTaskBusy,

    // Rethrow: the form owns the error and must stay open to show it.
    createTask: (input: CreateTaskRequest) => createMutation.mutateAsync(input),
    updateTask: (id: string, patch: UpdateTaskRequest) =>
      updateMutation.mutateAsync({ id, patch }),
    testTask: (input: TestTaskRequest) => testMutation.mutateAsync(input),

    // Swallowed: fired from the list, where there is no field to blame, so the
    // failure surfaces through `error` above instead. The mutation's own
    // `.error` state still updates for that even though the caller here never
    // sees the rejection. `async`/`await`/`catch` rather than
    // `.mutateAsync(...).catch(() => {})` so the return type is cleanly
    // `Promise<void>` -- a `.catch` callback returning nothing still leaves the
    // promise typed as `T | void`, not `void`.
    toggleTaskEnabled: async (id: string, enabled: boolean): Promise<void> => {
      try {
        await toggleMutation.mutateAsync({ id, enabled });
      } catch {
        // Reported through `error` above.
      }
    },
    runTaskNow: async (id: string): Promise<void> => {
      try {
        await runNowMutation.mutateAsync(id);
      } catch {
        // Reported through `error` above.
      }
    },
    deleteTask: async (id: string): Promise<void> => {
      try {
        await deleteMutation.mutateAsync(id);
      } catch {
        // Reported through `error` above.
      }
    },
  };
}
