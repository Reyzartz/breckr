import { createRootRoute, Link, Outlet } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { Button, Text } from "brake-ui";
import { Moon, RefreshCw, Sun } from "lucide-react";
import { useTheme } from "../hooks/useTheme.ts";

const NAV_LINKS = [
  { to: "/", label: "Dashboard" },
  { to: "/runs", label: "Run history" },
  { to: "/channels", label: "Channels" },
] as const;

function RootLayout() {
  const { theme, toggleTheme } = useTheme();
  const queryClient = useQueryClient();

  return (
    <div className="mx-auto flex h-screen flex-col px-10 py-6">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <Text variant="h2" as="h1">
            Web Task Monitor
          </Text>
          <Text variant="caption" color="muted">
            Scheduled browser checks, conditions, and alerts
          </Text>
        </div>

        <div className="flex items-center gap-4">
          <nav className="flex items-center gap-1">
            {NAV_LINKS.map((link) => (
              <Link
                key={link.to}
                to={link.to}
                activeOptions={{ exact: link.to === "/" }}
                className="rounded-md px-3 py-1.5 text-sm text-text-muted transition-colors hover:bg-surface-hover hover:text-text"
                activeProps={{ className: "!text-text font-medium bg-surface-hover" }}
              >
                {link.label}
              </Link>
            ))}
          </nav>

          <div className="flex items-center gap-2">
            <Button
              size="sm"
              variant="ghost"
              icon={RefreshCw}
              onClick={() => void queryClient.invalidateQueries()}
            >
              Refresh
            </Button>
            <Button
              size="sm"
              variant="ghost"
              icon={theme === "dark" ? Sun : Moon}
              onClick={toggleTheme}
              aria-label="Toggle theme"
            />
          </div>
        </div>
      </header>

      <div className="mt-4 min-h-0 flex-1">
        <Outlet />
      </div>
    </div>
  );
}

export const Route = createRootRoute({ component: RootLayout });
