import type { ErrorResponse } from "../types/index.ts";
import { config } from "../config/index.ts";

/** An API call that returned a non-2xx response, carrying the status. */
export class ApiError extends Error {
  readonly status: number;
  /**
   * Which form field the server blamed, when it blamed one. Lets the task form
   * show a validation message against the offending control instead of as a
   * page-level banner.
   */
  readonly field: string | undefined;

  constructor(message: string, status: number, field?: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.field = field;
  }
}

function isErrorResponse(value: unknown): value is ErrorResponse {
  return (
    typeof value === "object" &&
    value !== null &&
    "error" in value &&
    typeof (value as ErrorResponse).error === "string"
  );
}

/** The server wraps every successful body as `{ data }`. */
interface DataEnvelope<T> {
  data: T;
}

function isDataEnvelope<T>(value: unknown): value is DataEnvelope<T> {
  return typeof value === "object" && value !== null && "data" in value;
}

/**
 * Single place where HTTP becomes either data or a thrown ApiError.
 *
 * Success bodies arrive as `{ data }` and are unwrapped here, so nothing
 * downstream has to know the envelope exists. Failures arrive as
 * `{ error, field }` at the top level — deliberately not inside `data`, so a
 * body is unambiguously one or the other. Anything else (a proxy error page, a
 * dev server that isn't up) falls back to the status line, so the UI never ends
 * up displaying "undefined".
 */
export async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`${config.apiBasePath}${path}`, {
    ...init,
    headers: {
      // Only claim a JSON body when there is one — a bodyless POST like
      // run-now that declares content-type: application/json invites the
      // server to try parsing nothing.
      ...(init?.body === undefined ? {} : { "content-type": "application/json" }),
      ...init?.headers,
    },
  });

  if (!response.ok) {
    const body: unknown = await response.json().catch(() => null);
    const message = isErrorResponse(body)
      ? body.error
      : `${String(response.status)} ${response.statusText}`;
    throw new ApiError(message, response.status, isErrorResponse(body) ? body.field : undefined);
  }

  // DELETE answers 204 with no body at all, and response.json() would throw on
  // it. Callers of those routes ask for `void`.
  if (response.status === 204) return undefined as T;

  const body: unknown = await response.json();
  return (isDataEnvelope<T>(body) ? body.data : body) as T;
}

/** Message suitable for showing a user, from any thrown value. */
export function toErrorMessage(err: unknown): string {
  if (err instanceof Error) return err.message;
  return String(err);
}
