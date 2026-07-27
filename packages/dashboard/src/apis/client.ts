import type { ErrorResponse } from "@breckr/shared";
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

/**
 * Single place where HTTP becomes either data or a thrown ApiError.
 *
 * The API reports failures as `{ error }`; anything else (a proxy error page,
 * a dev server that isn't up) falls back to the status line, so the UI never
 * ends up displaying "undefined".
 */
export async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`${config.apiBasePath}${path}`, {
    ...init,
    headers: {
      // Only claim a JSON body when there is one. Fastify rejects an empty body
      // sent with content-type: application/json (FST_ERR_CTP_EMPTY_JSON_BODY),
      // which is exactly what a bodyless POST like run-now sends.
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

  return response.json() as Promise<T>;
}

/** Message suitable for showing a user, from any thrown value. */
export function toErrorMessage(err: unknown): string {
  if (err instanceof Error) return err.message;
  return String(err);
}
