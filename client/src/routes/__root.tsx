import { createRootRouteWithContext, Outlet } from "@tanstack/react-router";
import type { QueryClient } from "@tanstack/react-query";

/**
 * Providers only, and no chrome of its own.
 *
 * The header, the nav and the live socket all moved down to `_authed`, so that
 * the login page -- which is outside it -- renders on its own rather than
 * inside a shell whose data it is not yet allowed to fetch.
 *
 * The context is what lets a route's `beforeLoad` reach the query cache, which
 * is how the auth guard resolves the session without a second request.
 */
export const Route = createRootRouteWithContext<{ queryClient: QueryClient }>()({
  component: () => <Outlet />,
});
