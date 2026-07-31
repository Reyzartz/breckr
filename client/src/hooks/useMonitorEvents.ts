import { useEffect, useRef, useState } from "react";
import { eventsUrl, parseChangeEvent } from "../apis/events.api.ts";
import { config } from "../config/index.ts";
import type { MonitorResource } from "../types/index.ts";

export type ConnectionState = "connecting" | "open" | "reconnecting";

interface UseMonitorEventsOptions {
  /**
   * Called with the resources that changed, and with `undefined` — meaning all
   * of them — whenever a full resync is owed.
   *
   * A resync is owed on every connect, because a socket that has just come up
   * cannot know what it missed while it was down. Loading the initial data from
   * there rather than separately is deliberate: it means the subscription is
   * already live before the first fetch, so there is no window in which a change
   * is announced to nobody.
   */
  onChange: (resources: readonly MonitorResource[] | undefined) => void;
}

/**
 * Owns the live connection, and nothing else.
 *
 * The dashboard has no polling loop: this socket is the only thing that tells it
 * to refetch. That makes staying connected the whole job — a socket that dropped
 * silently would leave the page frozen on stale data with no timer to save it,
 * so every close reconnects and every reconnect resyncs.
 */
export function useMonitorEvents({
  onChange,
}: UseMonitorEventsOptions): ConnectionState {
  const [connection, setConnection] = useState<ConnectionState>("connecting");

  // Held in a ref so the effect below depends on nothing and never tears the
  // socket down to pick up a new render's callback.
  const onChangeRef = useRef(onChange);
  onChangeRef.current = onChange;

  useEffect(() => {
    let socket: WebSocket | null = null;
    let retry: number | undefined;
    let attempt = 0;
    let everOpened = false;
    // StrictMode mounts effects twice in development. Without this the first
    // cleanup would leave the second connect's timer running against a closed
    // socket.
    let disposed = false;

    const connect = () => {
      if (disposed) return;

      socket = new WebSocket(eventsUrl());

      socket.onopen = () => {
        attempt = 0;
        everOpened = true;
        setConnection("open");
        // Everything, because whatever changed while this socket was down was
        // announced to nobody. On the first connect this is also what loads the
        // page.
        onChangeRef.current(undefined);
      };

      socket.onmessage = (event: MessageEvent<unknown>) => {
        const change = parseChangeEvent(event.data);
        if (change) onChangeRef.current(change.resources);
      };

      // onerror is always followed by onclose, so scheduling the retry from
      // one place avoids racing two reconnects against each other.
      socket.onclose = () => {
        const neverOpened = !everOpened;
        socket = null;
        if (disposed) return;

        setConnection("reconnecting");
        scheduleReconnect();

        // The very first attempt failing means the page has no data and nothing
        // else is going to fetch it. Load it over plain HTTP instead, so a
        // dashboard behind something that strips websocket upgrades still works
        // -- as a stale one that says so, rather than as a blank page.
        if (neverOpened) {
          everOpened = true;
          onChangeRef.current(undefined);
        }
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
