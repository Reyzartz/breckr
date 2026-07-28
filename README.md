# Web Task Monitor

Self-hosted service that runs browser tasks on a schedule, checks a condition
against what it extracts, sends a Telegram alert when the condition is met, and
serves a dashboard of run history.

Tasks are created from the dashboard — no code, no rebuild.

## How it works

One process. The Go server owns the cron registry, so "Run now" in the dashboard
is a direct function call rather than a hop to a separate scheduler.

```
cron tick ──┐
            ├─> runner ──> browser (CDP) ──> extract ──> SQLite
run-now   ──┘                                   │
                                                └─> condition ──> Telegram
```

A task is a **declarative spec** stored in SQLite — a URL, a CSS selector, what
to pull out of it, and how to test the result. One generic executor interprets
it at run time. Nothing you type is ever evaluated as code, which is what makes
it safe to author tasks from a dashboard that has no authentication in front
of it.

Go backend, TypeScript dashboard:

| | |
|---|---|
| `server/` | Go. Scheduler + API + the browser driver. `main.go` → `internal/{app,routes,api,…}`. |
| `client/` | React + Vite on `brake-ui`. Layered: `components` → `hooks` → `services` → `apis`. |

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
| `notifier` | Telegram |
| `types` | the HTTP contract, mirrored by `client/src/types` |

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

## Tasks

Press **Add task** in the dashboard. A task answers four questions:

| | |
|---|---|
| **Where** | a `http`/`https` URL, and a CSS selector on that page |
| **What** | `text`, `number`, `attribute`, `count` (how many match) or `exists` |
| **When to alert** | an operator and a value — `is less than 100`, `contains "in stock"`, `changed since the last run` |
| **What to say** | an optional message template |

Only the operators that make sense for the chosen extraction are offered, and
the server rejects an invalid pairing on save rather than letting it become a
condition that can never fire.

The message template supports `{{value}}`, `{{raw}}`, `{{url}}` and `{{name}}`.
It is filled in by substitution, never evaluated.

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

### Notifications are edge-triggered

You are alerted once when the condition flips false → true, and not again until
it goes back to false. Prefer a plain state test (`price is less than 100`) —
the framework handles the transition. State is persisted, so restarting does not
re-alert. Editing a task's definition re-arms it, since the stored state
described the *old* condition.

`changed` is the exception that still composes correctly: the run after a change
sees no change, which re-arms it for the next one. It compares against the last
**successful** run, and reads as "no change" before the first one — so a new
task never alerts on the first thing it sees.

If Telegram is configured but a send fails, the alert is retried on the next run
rather than being swallowed. If Telegram is not configured at all, messages are
logged and dedup behaves exactly as it would in production.

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
cp .env.example .env   # fill in TELEGRAM_*, TZ, etc.
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

All in `.env` (see `.env.example`). `BROWSER_WS_ENDPOINT`,
`TELEGRAM_BOT_TOKEN` / `TELEGRAM_CHAT_ID` (both or neither), `PORT`, `HOST`,
`DB_PATH`, `TZ` (cron uses wall-clock time in this zone),
`DEFAULT_TIMEOUT_MS`, `RUN_RETENTION_DAYS`, `CLIENT_DIST` (the dashboard build to
serve), `CLIENT_ALLOWED_ORIGIN`. Missing or contradictory values fail at boot
rather than at the first tick.

Relative `DB_PATH` and `CLIENT_DIST` resolve against the directory holding the
`.env`, so they work whether the binary runs from the repo root or from
`server/`.

## API

| Route | |
|---|---|
| `GET /api/health` | liveness + whether the browser is reachable |
| `GET /api/tasks` | tasks with last run and next run time |
| `POST /api/tasks` | create; schedules it immediately |
| `PATCH /api/tasks/:id` | any of `{ enabled, name, schedule \| cron_expr, spec }` |
| `DELETE /api/tasks/:id` | delete; run history cascades with it |
| `POST /api/tasks/test` | run a draft spec once — no run row, no notification |
| `GET /api/runs` | `task_id`, `status`, `limit` (max 200), `offset` |
| `GET /api/runs/:id` | full result / error |
| `POST /api/tasks/:id/run-now` | trigger immediately (works while disabled) |

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
