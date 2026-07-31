# Web Task Monitor

Self-hosted service that runs browser tasks on a schedule, checks a condition
against what it extracts, alerts you when the condition is met, and serves a
dashboard of run history.

Alerts go to any number of channels — Telegram, Discord, Slack, email, or your
own webhook — created from the dashboard and picked per task.

Tasks are created from the dashboard — no code, no rebuild.

## How it works

One process. The Go server owns the cron registry, so "Run now" in the dashboard
is a direct function call rather than a hop to a separate scheduler.

```
cron tick ──┐
            ├─> runner ──> browser (CDP) ──> extract ──> SQLite
run-now   ──┘                                   │
                                                └─> condition ──> dispatcher ──┬─> Telegram
                                                                               ├─> Discord
                                                                               ├─> Slack
                                                                               ├─> email
                                                                               └─> webhook
```

A task is a **declarative spec** stored in SQLite — a URL, and one or more
conditions, each a CSS selector, what to pull out of it, and how to test the
result. One generic executor interprets it at run time. Nothing you type is ever
evaluated as code, which is what makes it safe to author tasks from a dashboard
that has no authentication in front of it.

Go backend, TypeScript dashboard:

| | |
|---|---|
| `server/` | Go. Scheduler + API + the browser driver. `main.go` → `internal/{app,routes,api,…}`. |
| `client/` | React + Vite on `brake-ui`, routed with TanStack Router and TanStack Query on axios. |

Inside `server/internal`:

| | |
|---|---|
| `api`, `routes`, `middleware` | HTTP: chi router, handlers, request logging |
| `app` | wiring and the boot/shutdown sequence |
| `store`, `migrations` | SQLite via `database/sql`, schema through goose |
| `executor` | spec validation, the schedule ⇄ cron mapping, extraction and the operator table |
| `scheduler` | the live cron registry |
| `runner` | the edge-trigger state machine |
| `browser` | the CDP connection and the one mutex every run passes through |
| `notifier` | a transport per channel kind, and the dispatcher that fans an alert out to all of a task's channels |
| `crypto` | AES-GCM at rest for channel credentials |
| `types` | the HTTP contract, mirrored by `client/src/types` |

Inside `client/src`:

| | |
|---|---|
| `routes` | one file per page — `/` (dashboard), `/runs` (history), `/channels` — plus `__root.tsx` for the shared header/nav. `@tanstack/router-plugin` generates `routeTree.gen.ts` from this folder; it's gitignored, not checked in. |
| `hooks` | one `use<Resource>` per resource (`useTasks`, `useRuns`, `useHealth`, `useChannels`), each a thin TanStack Query wrapper: the query plus every mutation on it. `useMonitorEvents`, mounted once in `__root.tsx`, is what keeps them fresh — no query polls on its own timer; the server pushes a "these resources changed" signal over `/api/events` and this invalidates exactly the query keys it names. Components stay presentational. |
| `services/api` | one axios-backed `<Resource>Service` class per resource, all extending `ApiClient` in `base.ts`, which unwraps the `{ data }` envelope and turns a failure into an `ApiError` carrying the server's `field`. |
| `components` | presentational, prop-driven; unchanged in shape by the router migration. |
| `constants/queryKeys.ts` | one array root per resource — `[...QueryKeys.runs, filters]` is how a hook narrows to its own cache entry without a naming collision. |
| `types` | see [The contract on two sides](#the-contract-on-two-sides). |

## Setup

Needs Go 1.25+ and Node 20+.

```bash
cp .env.example .env && cd client && npm install
```

Start a browser (see [Browsers](#browsers)), then the server:

```bash
make start-server
```

The API is on `:3000`. For dashboard development run Vite separately — it
proxies `/api` through:

```bash
make start-client
```

`make start` runs both. Other targets: `make build`, `make typecheck`,
`make test`, `make docker-up`.

## Pages

| Route | |
|---|---|
| `/` | Tasks, the warnings that matter before you wait on an alert (browser down, nothing configured to notify, a task with no channels), and the last few runs |
| `/runs` | The full run history — filters and paging are URL search params (`?taskId=…&status=…&offset=…`), so a filtered view is a link you can send someone, not state that resets on reload |
| `/channels` | Create, edit, mute, delete and test delivery destinations |

Creating and editing a task stays a modal reachable from `/` rather than its own
route: a spec is complex enough that a full-page context switch would lose the
task list you're authoring it against.

## Tasks

Press **Add task** in the dashboard. A task answers four questions:

| | |
|---|---|
| **Where** | a `http`/`https` URL |
| **What** | one or more conditions, each a CSS selector plus `text`, `number`, `attribute`, `count` (how many match) or `exists` |
| **When to alert** | per condition, an operator and a value — `is less than 100`, `contains "in stock"`, `changed since the last run` |
| **What to say** | an optional message template |

Only the operators that make sense for the chosen extraction are offered, and
the server rejects an invalid pairing on save rather than letting it become a
condition that can never fire.

### More than one condition

A task can watch up to ten things, all on the same page — one navigation per
run. They combine with **all of these are true** or **any of these is true**,
chosen once for the whole task; there is no nesting. Watching two sites is two
tasks.

The combined answer is what drives the alert, so a task set to `all` alerts once
when the last of its conditions starts matching. Each condition's own outcome is
recorded on the run, so run history says which one changed.

`changed` keys its history on what a condition extracts rather than on its
position, so reordering the list does not make one condition compare against
another's last value. Editing a condition simply leaves it with nothing to
compare against, which reads as no change — at most one skipped alert, never a
spurious one.

The message template supports `{{value}}`, `{{raw}}`, `{{url}}` and `{{name}}`,
plus `{{value1}}` / `{{raw1}}` … one pair per condition. `{{value}}` is the
first condition's, so a template written when the task had one keeps working. An
index the task has no condition for is rejected on save. Templates are filled in
by substitution, never evaluated.

A spec stored before conditions became a list is read back as a task with one —
the flat shape is hoisted on decode, so nothing had to be rewritten in the
database, and an API client still sending it keeps working.

Tasks are stored in SQLite and scheduled the moment you save them — **no
rebuild, no restart**, in development or in production. Editing the schedule
re-arms the cron entry live.

The task **ID** is fixed once created: run history is keyed on it, so changing
it would orphan the history.

### Test before you save

The **Test** button runs the draft against the real browser and shows you what
came back, whether the condition matched, and the alert it would have sent. It
writes no run row and sends no notification, so press it as often as you like
while getting a selector right.

This is also the quickest end-to-end check that the browser is wired up
correctly.

### Notifications are edge-triggered by default

You are alerted once when the condition flips false → true, and not again until
it goes back to false. Prefer a plain state test (`price is less than 100`) —
the framework handles the transition. State is persisted, so restarting does not
re-alert. Editing a task's definition re-arms it, since the stored state
described the *old* condition.

Each task chooses this with **Alert me**, saved as `notify_mode`:

| mode | behaviour |
| --- | --- |
| `transition` | once on the false → true edge, then quiet until it clears (**default**) |
| `always` | on every scheduled run where the condition is true |

Pick `always` for a task where each matching run is its own event; leave it alone
otherwise, since a repeating alert is the fastest way to stop reading them.

The mode is stored on the task rather than in its spec, so it survives an edit to
the condition, and a `PATCH` carrying only `notify_mode` changes when the task
alerts without touching *whether* it currently matches. Saving from the dashboard
submits the whole spec, so it re-arms the trigger like any other definition edit.

`changed` is the exception that still composes correctly: the run after a change
sees no change, which re-arms it for the next one. It compares against the last
**successful** run, and reads as "no change" before the first one — so a new
task never alerts on the first thing it sees.

A task's channels are all tried in parallel. One success counts as delivered —
retrying for the sake of a failed channel would re-alert the ones that already
worked, and duplicate alerts erode trust faster than a missing one. The failures
are still recorded per channel and shown on the run, so a permanently broken
channel is visible rather than merely retried.

If *every* channel fails, the alert is still owed: the trigger stays disarmed and
the next run retries it. If a task has no channels at all, messages are logged and
dedup behaves exactly as it would in production. Both hold in either mode.

### Notification channels

Channels are rows, not environment variables — created in the dashboard, editable
without a restart. Each is one of Telegram, Discord, Slack, email (Gmail SMTP by
default) or a custom webhook, and each task picks any number of them.

**Test** on a channel sends one real message through the same path a real alert
takes, so a token the API rejects is caught before it costs you an alert. You can
test a config before saving it.

Credentials are encrypted at rest with AES-GCM. The key is generated on first
boot into `secret.key` beside the database, mode `0600` — back the two up
together, since the database alone cannot be read without it. A channel whose key
no longer matches is shown as **needs credentials** rather than disappearing.
Secrets are never returned by the API: the dashboard sees them masked, and
leaving a masked field untouched keeps what is stored.

Muting a channel keeps it attached to its tasks but skips it when alerting.
Deleting one keeps the run history it appears in, under the name it had.

### Tasks with no definition

A task can exist with no usable spec — a row written by an older version, or one
whose stored JSON was corrupted. It keeps its run history and is shown as **no
definition**, but cannot be run or edited; only deleted. Such a row is logged at
boot and skipped, deliberately *not* failing the boot: refusing to start would
lock you out of the only UI that can clean it up.

## Browsers

Both speak CDP, so switching is one line in `.env` — no code changes.

For local development (`make start-server` on the host) start only the browser,
not the app container:

| | Lightpanda (default) | Chrome (fallback) |
|---|---|---|
| Start | `docker compose up -d lightpanda` | `docker compose --profile chrome up -d chrome` |
| `BROWSER_WS_ENDPOINT` | `ws://127.0.0.1:9222` | `http://127.0.0.1:9223` |
| Speed / memory | very fast, light | heavier |
| Screenshots, PDF, WebGL | no — it never renders | yes |
| Web API coverage | partial (Beta) | complete |

Chrome takes an `http://` address because its browser socket carries a
per-launch UUID; the server resolves the real endpoint itself via
`/json/version`.

**If a site returns empty results or throws on Lightpanda, that is the expected
coverage failure** — start Chrome, change the one line, and re-run.

> **Security:** an exposed CDP port is remote code execution. Both services
> publish to `127.0.0.1` only. Never widen that to `0.0.0.0` to fix a connection
> problem.

## Deployment (Ubuntu)

One command builds and runs everything — the app and the browser it drives:

```bash
git clone <repo> && cd breckr
cp .env.example .env   # fill in BROWSER_WS_ENDPOINT, TZ, etc.
docker compose up -d --build
```

`docker compose ps` should show `web-task-monitor` and `lightpanda` running. The
image builds the dashboard and the Go binary in separate stages and ships one
runtime container; the server serves the built dashboard from its own origin, so
the whole thing is one port and nginx is optional. Compose runs exactly one
instance of the app — a second would fight the first over the browser and double
every scheduled run.

Redeploy after pulling changes. Tasks themselves need no redeploy — they live in
the database:

```bash
git pull && docker compose up -d --build
```

Logs: `docker compose logs -f app`. The SQLite database lives in `./data`,
bind-mounted into the container, so it survives `docker compose down` and
image rebuilds.

## The contract on two sides

`server/internal/types` is the authority. `client/src/types/index.ts` mirrors it
as TypeScript declarations and nothing else — no generator, no build step, just
two files that have to agree. The json tags on the Go structs are what pin them
together, and they are deliberately inconsistent (`cron_expr` beside
`waitForSelector`) because that is what the wire format already was.

Runtime tables that both sides need — which operators go with which extraction
kind — live in `server/internal/types/constants.go` and, separately, in
`client/src/constants/`. The server's copy is the one that decides; the client's
only stops the form offering a pairing the server would reject anyway.

## Configuration

All in `.env` (see `.env.example`). `BROWSER_WS_ENDPOINT`, `PORT`, `HOST`,
`DB_PATH`, `TZ` (cron uses wall-clock time in this zone),
`DEFAULT_TIMEOUT_MS`, `RUN_RETENTION_DAYS`, `CLIENT_DIST` (the dashboard build to
serve), `CLIENT_ALLOWED_ORIGIN`, `SECRET_KEY_FILE` (defaults to `secret.key`
beside the database). Missing or contradictory values fail at boot rather than at
the first tick.

Notification credentials are deliberately absent: channels are managed from the
dashboard and stored encrypted in the database.

Relative `DB_PATH` and `CLIENT_DIST` resolve against the directory holding the
`.env`, so they work whether the binary runs from the repo root or from
`server/`.

## API

| Route | |
|---|---|
| `GET /api/health` | liveness + whether the browser is reachable |
| `GET /api/tasks` | tasks with last run and next run time |
| `POST /api/tasks` | create; schedules it immediately |
| `PATCH /api/tasks/:id` | any of `{ enabled, name, schedule \| cron_expr, spec, notify_mode, channel_ids }` |
| `DELETE /api/tasks/:id` | delete; run history cascades with it |
| `POST /api/tasks/test` | run a draft spec once — no run row, no notification |
| `GET /api/runs` | `task_id`, `status`, `limit` (max 200), `offset` |
| `GET /api/runs/:id` | full result / error, plus the per-channel `attempts` |
| `POST /api/tasks/:id/run-now` | trigger immediately (works while disabled) |
| `GET /api/channels` | channels with their secrets masked |
| `POST /api/channels` | create `{ name, type, config, enabled? }` |
| `PATCH /api/channels/:id` | any of `{ name, config, enabled }`; omitted secrets are kept |
| `DELETE /api/channels/:id` | delete; task links cascade, history is kept |
| `POST /api/channels/test` | send a test through an unsaved `{ type, config }` |
| `POST /api/channels/:id/test` | send a test through a saved channel |

Every successful response is `{ "data": … }`. Failures are `{ "error": … }` at
the top level, with a `field` naming the control that was wrong when a spec
fails validation.

Creating or updating a task takes either a structured `schedule` — what the
dashboard's builder sends, converted to cron by the server — or a raw
`cron_expr`. `schedule` wins when both are present. Tasks are read back with a
`schedule` derived from the stored expression; anything the builder cannot
express comes back as `{ every: "custom", cron }` and is left alone by an edit.

## Testing

```bash
make test
```

Standard `go test`, no extra dependencies. The suite covers the behavior that is
easy to break by accident: the edge-trigger state machine through every delivery
outcome, mutex serialization, run timeouts, spec validation, every comparison
operator, the schedule ⇄ cron round trip, live registry mutation (register /
reschedule / unregister), and the storage guarantees (boot sweep, retention,
pagination, delete cascade).

Each test gets its own SQLite file under `t.TempDir()`, so nothing touches
`data/monitor.db` and nothing has to run serially.

## Notes

- Runs are serialized: Lightpanda's CDP server accepts one connection, one
  context and one page at a time. `SkipIfStillRunning` stops a task overlapping
  itself; an in-process mutex stops different tasks colliding.
- A run row is written *before* execution, so a crash stays visible instead of
  vanishing. Rows left `running` are marked failed at the next boot.
- Runs older than `RUN_RETENTION_DAYS` are pruned at boot and daily at 04:00.
- `GET /api/runs` returns `condition_met` and `notified` as real booleans; the
  store converts SQLite's 0/1 at the boundary so the contract is honest.
- The schema is applied by goose at boot. A database written by the previous Node
  server migrates in place: every table is created `IF NOT EXISTS`, so the first
  run just stamps the version.
