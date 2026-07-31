import axios, { AxiosError } from "axios";
import { config } from "../../config/index.ts";
import type { ErrorResponse } from "../../types/index.ts";

/**
 * An API call that returned a non-2xx response, carrying the status.
 *
 * `field` is which form field the server blamed, when it blamed one. It lets
 * the task and channel forms show a validation message against the offending
 * control instead of as a page-level banner.
 */
export class ApiError extends Error {
  readonly status: number;
  readonly field: string | undefined;

  constructor(message: string, status: number, field?: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.field = field;
  }
}

/** Message suitable for showing a user, from any thrown value. */
export function toErrorMessage(err: unknown): string {
  if (err instanceof Error) return err.message;
  return String(err);
}

function isErrorResponse(value: unknown): value is ErrorResponse {
  return (
    typeof value === "object" &&
    value !== null &&
    "error" in value &&
    typeof (value as ErrorResponse).error === "string"
  );
}

/**
 * The one axios instance every service shares.
 *
 * The API is same-origin in both modes -- Vite proxies /api in development,
 * and the Go server serves this build itself in production -- so the base URL
 * is a relative prefix rather than an absolute one, and there is no CORS to
 * think about either way.
 *
 * The timeout is generous rather than cha-ching's flat 10s: `/tasks/test` and
 * `/tasks/:id/run-now` drive a real browser and can legitimately take up to
 * DEFAULT_TIMEOUT_MS (30s) plus connect time, and the Go server's own
 * WriteTimeout is set to exactly that budget plus 30s of slack. A client
 * timeout shorter than the server's would cut off a request the server was
 * always going to finish.
 */
const client = axios.create({
  baseURL: config.apiBaseUrl,
  timeout: config.apiTimeoutMs,
  // A no-op in both of the modes above, since same-origin requests send cookies
  // regardless. It is here so that anyone who does serve the dashboard from a
  // different origin gets the session cookie sent, rather than a puzzling 401
  // against a server that already sets AllowCredentials.
  withCredentials: true,
});

/**
 * What to do when the server says the session is gone.
 *
 * Set from `main.tsx` rather than imported, because this module is deliberately
 * React-free: it cannot reach `useNavigate`, and a `window.location` assignment
 * would turn an expired session into a full page reload in the middle of
 * whatever the user was doing.
 */
let onUnauthorized: (() => void) | null = null;

export function setUnauthorizedHandler(handler: () => void) {
  onUnauthorized = handler;
}

/**
 * Runs that handler. Called by the interceptor below, and by the event socket,
 * which discovers the same fact a different way -- its handshake is rejected
 * before there is any response for an interceptor to see.
 */
export function notifyUnauthorized() {
  onUnauthorized?.();
}

/**
 * Normalizes every rejection into an ApiError, in one place.
 *
 * Failures arrive as `{ error, field }` at the top level -- deliberately not
 * inside `data`, so a body is unambiguously a success or a failure. Anything
 * else (a proxy error page, a dev server that isn't up) falls back to the
 * status line, so the UI never ends up displaying "undefined".
 */
client.interceptors.response.use(
  (response) => response,
  (axiosError: AxiosError<unknown>) => {
    const body: unknown = axiosError.response?.data;
    const status = axiosError.response?.status ?? 0;

    const message = isErrorResponse(body)
      ? body.error
      : axiosError.message || `Request failed with status ${String(status)}`;

    // Everything under /auth/ is excluded, and that exclusion is load-bearing:
    // a wrong password is itself a 401, so without it every failed login would
    // bounce the user to the login page they are already on and throw away the
    // message telling them what went wrong.
    if (status === 401 && !(axiosError.config?.url ?? "").startsWith("/auth/")) {
      notifyUnauthorized();
    }

    return Promise.reject(
      new ApiError(message, status, isErrorResponse(body) ? body.field : undefined)
    );
  }
);

/** The server wraps every successful body as `{ data }`. */
interface DataEnvelope<T> {
  data: T;
}

/**
 * Base class every resource service extends.
 *
 * Each method unwraps the `{ data }` envelope so callers work with plain
 * response types, the same as the hand-rolled `request<T>` this replaces.
 */
export class ApiClient {
  protected async get<T>(path: string): Promise<T> {
    const response = await client.get<DataEnvelope<T>>(path);
    return response.data.data;
  }

  protected async post<T>(path: string, body?: unknown): Promise<T> {
    const response = await client.post<DataEnvelope<T>>(path, body);
    return response.data.data;
  }

  protected async patch<T>(path: string, body?: unknown): Promise<T> {
    const response = await client.patch<DataEnvelope<T>>(path, body);
    return response.data.data;
  }

  /** DELETE answers 204 with no body at all, so there is nothing to unwrap. */
  protected async delete(path: string): Promise<void> {
    await client.delete(path);
  }

  /** As above, for the one POST that answers 204 rather than an envelope. */
  protected async postNoContent(path: string, body?: unknown): Promise<void> {
    await client.post(path, body);
  }
}
