import {
  createFileRoute,
  Outlet,
  redirect,
  useLocation,
  useNavigate,
} from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { Button, Tab, TabGroup, TabList, Text } from "broke-ui";
import { LogOut, Moon, RefreshCw, Sun, WifiOff } from "lucide-react";
import { useTheme } from "../hooks/useTheme.ts";
import { useMonitorEvents } from "../hooks/useMonitorEvents.ts";
import { useAuth, authStatusQueryOptions } from "../hooks/useAuth.ts";

const NAV_LINKS = [
  { to: "/", label: "Dashboard" },
  { to: "/runs", label: "Run history" },
  { to: "/channels", label: "Channels" },
] as const;

function AuthedLayout() {
  const { theme, toggleTheme } = useTheme();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const { authRequired, logout } = useAuth();
  const { pathname } = useLocation();

  // Mounted once here rather than per-route, so navigating between routes
  // doesn't reconnect the socket -- every route's queries share this one
  // subscription.
  //
  // That it lives in *this* component rather than the root is what keeps the
  // socket from ever opening before the guard below has passed: an
  // unauthenticated handshake would only be rejected with a 401 anyway, and the
  // dashboard would sit there saying "Reconnecting…".
  const connection = useMonitorEvents();

  const signOut = () => {
    void logout().then(() => navigate({ to: "/login" }));
  };

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
            {/* No password configured means no session to end, so no button. */}
            {authRequired && (
              <Button
                variant="ghost"
                icon={LogOut}
                onClick={signOut}
                aria-label="Sign out"
              />
            )}
          </div>
        </div>

        <div className="flex items-center gap-3 sm:gap-4">
          {/*
            A segmented control on mobile: the three links split the width
            evenly, which makes each one a comfortably wide tap target instead
            of three small ones bunched at the left.
          */}

          <TabGroup
            value={pathname}
            onValueChange={(to) => navigate({ to })}
            className="w-full sm:w-auto"
          >
            <TabList>
              {NAV_LINKS.map((link) => (
                <Tab
                  key={link.to}
                  value={link.to}
                  className="w-full sm:w-auto justify-center"
                >
                  {link.label}
                </Tab>
              ))}
            </TabList>
          </TabGroup>

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
            {authRequired && (
              <Button
                size="sm"
                variant="ghost"
                icon={LogOut}
                onClick={signOut}
                aria-label="Sign out"
              />
            )}
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

/**
 * A pathless layout route: the `_` prefix contributes nothing to the URL, so
 * `/`, `/runs` and `/channels` are exactly where they were. What it adds is one
 * place for the guard to sit, in front of every page that needs a session.
 *
 * `ensureQueryData` rather than a fetch, so the answer is shared with `useAuth`
 * below instead of being requested twice.
 */
export const Route = createFileRoute("/_authed")({
  beforeLoad: async ({ context, location }) => {
    const status = await context.queryClient.ensureQueryData(
      authStatusQueryOptions,
    );
    if (status.required && !status.authenticated) {
      throw redirect({ to: "/login", search: { redirect: location.href } });
    }
  },
  component: AuthedLayout,
});
