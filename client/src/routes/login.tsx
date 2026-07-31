import { useState } from "react";
import { createFileRoute, redirect, useNavigate } from "@tanstack/react-router";
import { LoginForm } from "../components/LoginForm.tsx";
import { useAuth, authStatusQueryOptions } from "../hooks/useAuth.ts";
import { toErrorMessage } from "../services/api/index.ts";

interface LoginSearch {
  /** Where the guard bounced the user from, so they land back on it. */
  redirect?: string;
}

function LoginPage() {
  const navigate = useNavigate();
  const search = Route.useSearch();
  const { login, isLoggingIn } = useAuth();
  const [error, setError] = useState<string | null>(null);

  const handleSubmit = (password: string) => {
    setError(null);
    login(password)
      // A relative path only: `redirect` comes off the URL, so treating it as
      // anything else would make this an open redirect.
      .then(() => navigate({ to: search.redirect ?? "/" }))
      .catch((err: unknown) => {
        setError(toErrorMessage(err));
      });
  };

  return <LoginForm onSubmit={handleSubmit} error={error} isPending={isLoggingIn} />;
}

export const Route = createFileRoute("/login")({
  validateSearch: (search: Record<string, unknown>): LoginSearch => ({
    redirect:
      typeof search.redirect === "string" && search.redirect.startsWith("/")
        ? search.redirect
        : undefined,
  }),
  // A server with no password has no login page: there is nothing to type and
  // nothing to gain by typing it. Someone already signed in has no use for one
  // either.
  beforeLoad: async ({ context, search }) => {
    const status = await context.queryClient.ensureQueryData(authStatusQueryOptions);
    if (!status.required || status.authenticated) {
      throw redirect({ to: search.redirect ?? "/" });
    }
  },
  component: LoginPage,
});
