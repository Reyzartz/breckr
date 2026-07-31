import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClientProvider } from "@tanstack/react-query";
import { ReactQueryDevtools } from "@tanstack/react-query-devtools";
import { RouterProvider, createRouter } from "@tanstack/react-router";
import { TanStackRouterDevtools } from "@tanstack/react-router-devtools";
import { queryClient } from "./config/index.ts";
import { routeTree } from "./routeTree.gen.ts";
import { setUnauthorizedHandler } from "./services/api/base.ts";
import { QueryKeys } from "./constants/queryKeys.ts";
import "./index.css";

const router = createRouter({ routeTree, defaultPreload: "intent", context: { queryClient } });

/**
 * What happens when a session expires under a dashboard that is already open.
 *
 * The cache is corrected before the navigation, not after: `_authed`'s
 * `beforeLoad` reads exactly this entry, and leaving a stale "authenticated"
 * there would let it wave the user straight back through to a page whose every
 * query is about to 401 again.
 *
 * A router navigation rather than a location assignment, so an expired session
 * costs a re-render rather than a full page reload.
 */
setUnauthorizedHandler(() => {
  queryClient.setQueryData(QueryKeys.auth, { required: true, authenticated: false });
  void router.navigate({
    to: "/login",
    search: { redirect: window.location.pathname + window.location.search },
  });
});

// Registers the router's types globally, so `Link`, `useNavigate` and route
// hooks are typed against this app's actual routes everywhere they're used.
declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}

const container = document.getElementById("root");
if (!container) throw new Error("Root element #root not found in index.html");

createRoot(container).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
      {import.meta.env.DEV && (
        <>
          <ReactQueryDevtools initialIsOpen={false} />
          <TanStackRouterDevtools router={router} initialIsOpen={false} />
        </>
      )}
    </QueryClientProvider>
  </StrictMode>
);
