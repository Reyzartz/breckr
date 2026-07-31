import { useState, type FormEvent } from "react";
import { Button, Card, Input, Text } from "brake-ui";
import { Lock } from "lucide-react";

interface LoginFormProps {
  onSubmit: (password: string) => void;
  /** The server's message: a wrong password, or a closed rate-limit window. */
  error: string | null;
  isPending: boolean;
}

/**
 * The one password this server asks for.
 *
 * Presentational like the rest of `components/` — it holds the field's own text
 * and nothing else. Whether a login is even required, and where to go after
 * one, are the route's business.
 */
export function LoginForm({ onSubmit, error, isPending }: LoginFormProps) {
  const [password, setPassword] = useState("");

  const handleSubmit = (event: FormEvent) => {
    event.preventDefault();
    if (password && !isPending) onSubmit(password);
  };

  return (
    <div className="flex min-h-screen items-center justify-center px-4">
      <Card className="w-full max-w-sm">
        <form onSubmit={handleSubmit} className="flex flex-col gap-4">
          <div>
            <Text variant="h2" as="h1">
              Web Task Monitor
            </Text>
            <Text variant="small" color="muted">
              Enter the password for this server to continue.
            </Text>
          </div>

          <Input
            type="password"
            label="Password"
            icon={Lock}
            fullWidth
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            // The only field on the page, and the page exists to be typed into.
            autoFocus
            autoComplete="current-password"
            error={error ?? undefined}
            disabled={isPending}
          />

          <Button type="submit" fullWidth disabled={!password || isPending}>
            {isPending ? "Signing in…" : "Sign in"}
          </Button>
        </form>
      </Card>
    </div>
  );
}
