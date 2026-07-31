import { useEffect, useRef, useState } from "react";
import { useQueryClient, type QueryKey } from "@tanstack/react-query";
import { eventsUrl, parseChangeEvent } from "../services/api/events.ts";
import { authService } from "../services/api/auth.ts";
import { notifyUnauthorized } from "../services/api/base.ts";
import { config } from "../config/index.ts";
import { QueryKeys } from "../constants/queryKeys.ts";
import type { MonitorResource } from "../types/index.ts";

export type ConnectionState = "connecting" | "open" | "reconnecting";

const RESOURCE_QUERY_KEYS: Record<MonitorResource, QueryKey> = {
  tasks: QueryKeys.tasks,
  runs: QueryKeys.runs,
  health: QueryKeys.health,
  channels: QueryKeys.channels,
};

const ALL_RESOURCES = Object.keys(RESOURCE_QUERY_KEYS) as MonitorResource[];

/**
 * Owns the live connection, and nothing else.
 *
 * This is what replaced every query's `refetchInterval`: instead of each hook
 * polling on its own timer, the server pushes "these resources changed" and
 * this invalidates exactly the query keys it named -- `invalidateQueries` with
 * a resource's root key reaches every filtered variant under it (see
 * `QueryKeys`), so a paginated or filtered `useRuns` still catches up.
 *
 * The initial load is not this hook's job -- every `useQuery` already fetches
 * on mount regardless of the socket. This only invalidates on an actual change
 * message, and on reconnecting after having been open before: a socket that
 * dropped and came back cannot know what it missed while it was down, and the
 * queries mounted this whole time are exactly the ones that need to catch up.
 */
export function useMonitorEvents(): ConnectionState {
  const [connection, setConnection] = useState<ConnectionState>("connecting");
  const queryClient = useQueryClient();

  // Read inside the socket's callbacks without making the connect effect
  // depend on it, so the socket is never torn down to pick up a new
  // queryClient identity.
  const queryClientRef = useRef(queryClient);
  queryClientRef.current = queryClient;

  useEffect(() => {
    let socket: WebSocket | null = null;
    let retry: number | undefined;
    let attempt = 0;
    let everOpened = false;
    // StrictMode mounts effects twice in development. Without this the first
    // cleanup would leave the second connect's timer running against a closed
    // socket.
    let disposed = false;

    const invalidate = (resources: readonly MonitorResource[] | undefined) => {
      for (const resource of resources ?? ALL_RESOURCES) {
        void queryClientRef.current.invalidateQueries({
          queryKey: RESOURCE_QUERY_KEYS[resource],
        });
      }
    };

    const connect = () => {
      if (disposed) return;

      socket = new WebSocket(eventsUrl());

      socket.onopen = () => {
        attempt = 0;
        // Only a *re*connect owes a resync -- the first connect's queries are
        // already covered by their own mount-triggered fetch, and invalidating
        // here too would just fire a redundant one right behind it.
        const isReconnect = everOpened;
        everOpened = true;
        setConnection("open");
        if (isReconnect) invalidate(undefined);
      };

      socket.onmessage = (event: MessageEvent<unknown>) => {
        const change = parseChangeEvent(event.data);
        if (change) invalidate(change.resources);
      };

      socket.onclose = () => {
        socket = null;
        if (disposed) return;

        setConnection("reconnecting");

        // A restarting server and an expired session both close the socket, and
        // only one of them is worth interrupting the user over. Without asking,
        // an expired session reads as a permanent "Reconnecting…" against a
        // handshake that will 401 forever.
        //
        // The rejection branch is the important one: a server that is down
        // cannot answer this either, and that case must keep retrying rather
        // than bounce someone to a login page they do not need.
        void authService.fetchStatus().then(
          (status) => {
            if (status.required && !status.authenticated) {
              notifyUnauthorized();
            } else {
              scheduleReconnect();
            }
          },
          () => {
            scheduleReconnect();
          }
        );
      };
    };

    const scheduleReconnect = () => {
      if (disposed || retry !== undefined) return;

      // Exponential with jitter: a server coming back up should not be met by
      // every open tab reconnecting on the same tick.
      const backoff = Math.min(
        config.reconnectBaseMs * 2 ** attempt,
        config.reconnectMaxMs
      );
      attempt += 1;

      retry = window.setTimeout(() => {
        retry = undefined;
        connect();
      }, backoff * (0.5 + Math.random() / 2));
    };

    const reconnectNow = () => {
      if (disposed || document.visibilityState !== "visible") return;
      if (socket !== null) return;

      // A hidden tab has its timers throttled, so a backed-off retry may be
      // minutes overdue by the time the user looks at it again. Coming back to
      // the page should not mean waiting it out.
      window.clearTimeout(retry);
      retry = undefined;
      attempt = 0;
      connect();
    };

    connect();
    document.addEventListener("visibilitychange", reconnectNow);

    return () => {
      disposed = true;
      document.removeEventListener("visibilitychange", reconnectNow);
      window.clearTimeout(retry);
      socket?.close();
    };
  }, []);

  return connection;
}
