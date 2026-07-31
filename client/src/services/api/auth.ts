import { ApiClient } from "./base.ts";
import type { AuthStatusResponse, LoginRequest } from "../../types/index.ts";

/**
 * The session, such as it is: one shared password, and a cookie the server sets
 * and this code never sees. Nothing here handles a token, because the cookie is
 * HttpOnly — which is also why `/api/events` needs no help authenticating.
 */
export class AuthService extends ApiClient {
  fetchStatus(): Promise<AuthStatusResponse> {
    return this.get<AuthStatusResponse>("/auth/status");
  }

  /** Rejects with an ApiError on a wrong password (401) or a closed window (429). */
  login(password: string): Promise<{ ok: true }> {
    return this.post<{ ok: true }>("/auth/login", { password } satisfies LoginRequest);
  }

  logout(): Promise<void> {
    return this.postNoContent("/auth/logout");
  }
}

export const authService = new AuthService();
