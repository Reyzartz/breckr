import { createRootRoute, Link, Outlet } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { Button, Text } from "brake-ui";
import { Moon, RefreshCw, Sun, WifiOff } from "lucide-react";
import { useTheme } from "../hooks/useTheme.ts";
import { useMonitorEvents } from "../hooks/useMonitorEvents.ts";

const NAV_LINKS = [
  { to: "/", label: "Dashboard" },
  { to: "/runs", label: "Run history" },
  { to: "/channels", label: "Channels" },
] as const;

function RootLayout() {
  const { theme, toggleTheme } = useTheme();
  const queryClient = useQueryClient();
  // Mounted once here rather than per-route, so navigating between routes
  // doesn't reconnect the socket -- every route's queries share this one
  // subscription.
  const connection = useMonitorEvents();

  return (
    /*
      Two height models. Below xl the page scrolls the way a document does,
      which is what a phone expects and what lets the address bar collapse;
      from xl up it is a fixed shell whose panels scroll internally, so the
      two-column dashboard can put a long run list beside a long task list
      without the page itself growing.

      The width cap is what `mx-auto` was always missing: unbounded, a 2560px
      monitor stretched every row to its full width, stranding the title a
      metre from the nav and a task's toggle from its own Run button.
    */
    <div className="mx-auto flex min-h-screen w-full max-w-[1600px] flex-col px-4 py-4 sm:px-10 sm:py-6 xl:h-screen">
      <header className="flex flex-col gap-3 sm:flex-row sm:flex-wrap sm:items-center sm:justify-between">
        <div className="flex items-center justify-between gap-3">
          <div className="min-w-0">
            <Text variant="h2" as="h1" className="sm:text-xl">
              Web Task Monitor
            </Text>
            {/*
              Restating what the app is costs a phone two lines above the
              content it came for, so it starts at the width that has room.
              Sentence case rather than the uppercase `caption`: this is a
              sentence, and tracked-out capitals read as a label for the title
              rather than a description of it.
            */}
            <Text variant="small" color="muted" className="hidden sm:block">
              Scheduled browser checks, conditions, and alerts
            </Text>
          </div>

          {/* Beside the title on mobile; back with the nav once it fits. */}
          <div className="flex shrink-0 items-center gap-1 sm:hidden">
            <ConnectionNotice connection={connection} />
            <Button
              variant="ghost"
              icon={RefreshCw}
              onClick={() => void queryClient.invalidateQueries()}
              aria-label="Refresh"
            />
            <Button
              variant="ghost"
              icon={theme === "dark" ? Sun : Moon}
              onClick={toggleTheme}
              aria-label="Toggle theme"
            />
          </div>
        </div>

        <div className="flex items-center gap-3 sm:gap-4">
          {/*
            A segmented control on mobile: the three links split the width
            evenly, which makes each one a comfortably wide tap target instead
            of three small ones bunched at the left.
          */}
          <nav className="flex w-full items-center gap-1 rounded-lg bg-background-secondary p-1 sm:w-auto sm:bg-transparent sm:p-0">
            {NAV_LINKS.map((link) => (
              <Link
                key={link.to}
                to={link.to}
                activeOptions={{ exact: link.to === "/" }}
                className="flex-1 rounded-md px-3 py-2 text-center text-sm whitespace-nowrap text-text-muted transition-colors hover:bg-surface-hover hover:text-text sm:flex-none sm:py-1.5"
                activeProps={{
                  className: "!text-text font-medium bg-surface sm:bg-surface-hover",
                }}
              >
                {link.label}
              </Link>
            ))}
          </nav>

          <div className="hidden items-center gap-2 sm:flex">
            <ConnectionNotice connection={connection} />
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

/**
 * Shown only while disconnected. The dashboard has no polling loop, so a
 * dropped socket means what is on screen has stopped updating -- which the
 * user has no other way to tell.
 *
 * The wording collapses to an icon on mobile, where there is no room for it
 * beside the title, but the state still has to be visible.
 */
function ConnectionNotice({ connection }: { connection: string }) {
  if (connection === "open") return null;

  const label = connection === "connecting" ? "Connecting…" : "Reconnecting…";

  return (
    <Text variant="caption" color="muted">
      <span className="inline-flex items-center gap-1.5" title={label}>
        <WifiOff size={12} aria-hidden="true" />
        <span className="hidden sm:inline">{label}</span>
        <span className="sr-only sm:hidden">{label}</span>
      </span>
    </Text>
  );
}

export const Route = createRootRoute({ component: RootLayout });
