import { queryOptions, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { authService } from "../services/api/index.ts";
import { QueryKeys } from "../constants/queryKeys.ts";
import type { AuthStatusResponse } from "../types/index.ts";

/**
 * Shared with the router, which resolves this in `_authed`'s `beforeLoad`
 * through `ensureQueryData` — so the guard and the components below read one
 * cache entry rather than racing two requests for the same answer.
 */
export const authStatusQueryOptions = queryOptions({
  queryKey: QueryKeys.auth,
  queryFn: () => authService.fetchStatus(),
  /**
   * Unlike every other query here, this one does not watch live server state:
   * it changes when the user signs in or out, both of which write the cache
   * directly. Refetching it on every window focus would put a request in front
   * of each tab switch for an answer that has not moved.
   */
  staleTime: Infinity,
});

/** Writes the new state straight into the cache, so no refetch stands between
 * signing in and the redirect that follows it. */
function statusAfter(authenticated: boolean): AuthStatusResponse {
  return { required: true, authenticated };
}

/**
 * The session: whether one is needed, and the two mutations that start and end
 * it.
 *
 * `login` rethrows — the form owns that error, the same reason `useChannels`'s
 * create/update do. `logout` does not: it is fired from the header, where there
 * is nowhere sensible to render a failure, and a logout that fails still ends
 * with the user sent to the login page.
 */
export function useAuth() {
  const queryClient = useQueryClient();

  const statusQuery = useQuery(authStatusQueryOptions);

  const loginMutation = useMutation({
    mutationFn: (password: string) => authService.login(password),
    onSuccess: () => {
      queryClient.setQueryData(QueryKeys.auth, statusAfter(true));
    },
  });

  const logoutMutation = useMutation({
    mutationFn: () => authService.logout(),
    onSettled: () => {
      queryClient.setQueryData(QueryKeys.auth, statusAfter(false));
      // Everything cached was fetched under a session that no longer exists.
      // Clearing rather than invalidating so the next sign-in does not briefly
      // render the previous one's tasks.
      queryClient.removeQueries({ queryKey: QueryKeys.tasks });
      queryClient.removeQueries({ queryKey: QueryKeys.runs });
      queryClient.removeQueries({ queryKey: QueryKeys.channels });
      queryClient.removeQueries({ queryKey: QueryKeys.health });
    },
  });

  return {
    /** False when no password is configured: render no auth UI at all. */
    authRequired: statusQuery.data?.required ?? false,
    isAuthenticated: statusQuery.data?.authenticated ?? false,

    // Rethrow: the form owns the error.
    login: (password: string) => loginMutation.mutateAsync(password),
    isLoggingIn: loginMutation.isPending,

    logout: async (): Promise<void> => {
      try {
        await logoutMutation.mutateAsync();
      } catch {
        // The cookie is cleared in onSettled either way, and the caller
        // navigates to /login regardless.
      }
    },
  };
}
