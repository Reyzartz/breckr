import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClientProvider } from "@tanstack/react-query";
import { ReactQueryDevtools } from "@tanstack/react-query-devtools";
import { RouterProvider, createRouter } from "@tanstack/react-router";
import { TanStackRouterDevtools } from "@tanstack/react-router-devtools";
import { queryClient } from "./config/index.ts";
import { routeTree } from "./routeTree.gen.ts";
import "./index.css";

const router = createRouter({ routeTree, defaultPreload: "intent" });

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
