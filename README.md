# Web Task Monitor

Self-hosted service that runs browser tasks on a schedule, checks a condition
against what it extracts, sends a Telegram alert when the condition is met, and
serves a dashboard of run history.

Tasks are created from the dashboard — no code, no rebuild.

## How it works

One process. Fastify owns the cron registry, so "Run now" in the dashboard is a
direct function call rather than a hop to a separate scheduler.

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

TypeScript throughout, in three workspaces:

| Package | |
|---|---|
| `packages/server` | Scheduler + API. Layered: `apis` → `services` → `repositories`. See its [README](packages/server/src/README.md). |
| `packages/dashboard` | React + Vite on `brake-ui`. Layered: `components` → `hooks` → `services` → `apis`. |
| `packages/shared` | The HTTP contract. **Types only** — see [Shared types](#shared-types). |

## Setup

```bash
npm install && cp .env.example .env
```

Start a browser (see [Browsers](#browsers)), then:

```bash
npm run dev
```

The API is on `:3000`. For dashboard development, run Vite separately — it
proxies `/api` through:

```bash
npm run dev:dashboard
```

Other scripts: `npm run build` (both packages), `npm run typecheck`, `npm test`.

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

For local development (`npm run dev` on the host) start only the browser, not
the app container:

| | Lightpanda (default) | Chrome (fallback) |
|---|---|---|
| Start | `docker compose up -d lightpanda` | `docker compose --profile chrome up -d chrome` |
| `BROWSER_WS_ENDPOINT` | `ws://127.0.0.1:9222` | `http://127.0.0.1:9223` |
| Speed / memory | very fast, light | heavier |
| Screenshots, PDF, WebGL | no — it never renders | yes |
| Web API coverage | partial (Beta) | complete |

Chrome takes an `http://` address because its browser socket carries a
per-launch UUID; puppeteer resolves the real endpoint itself.

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

`docker compose ps` should show `web-task-monitor` and `lightpanda` running.
With `NODE_ENV=production` (set automatically for the container) Fastify
serves the built dashboard itself, so the whole thing is one port and nginx is
optional. Compose runs exactly one instance of the app — a second would fight
the first over the browser and double every scheduled run.

Redeploy after pulling changes. Tasks themselves need no redeploy — they live in
the database:

```bash
git pull && docker compose up -d --build
```

Logs: `docker compose logs -f app`. The SQLite database lives in `./data`,
bind-mounted into the container, so it survives `docker compose down` and
image rebuilds.

## Shared types

`packages/shared` declares the HTTP contract and deliberately publishes **no
runtime entry point**. npm workspaces symlink it into `node_modules`, and Node
refuses to strip types from there (`ERR_UNSUPPORTED_NODE_MODULES_TYPE_STRIPPING`)
— so a runtime export would fail at boot. Type-only imports erase completely and
are unaffected.

The practical rule: import types from `@breckr/shared`, and keep runtime values
in each package's own `constants/`, typed against those types. A value import
from the shared package fails at build, which is where you want to find out.

Relative imports inside the server carry a `.ts` extension; `tsc` rewrites them
to `.js` on emit. That is what lets `npm run dev` execute the sources directly
while production runs the compiled output — Node will not map a `.js` specifier
onto a `.ts` file, so the usual `nodenext` convention would break the dev loop.

## Configuration

All in `.env` (see `.env.example`). `BROWSER_WS_ENDPOINT`,
`TELEGRAM_BOT_TOKEN` / `TELEGRAM_CHAT_ID` (both or neither), `PORT`, `HOST`,
`DB_PATH`, `TZ` (cron uses wall-clock time in this zone),
`DEFAULT_TIMEOUT_MS`, `RUN_RETENTION_DAYS`. Missing or contradictory values fail
at boot rather than at the first tick.

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

A spec that fails validation comes back `400 { error, field }`, where `field`
names the control that was wrong.

Creating or updating a task takes either a structured `schedule` — what the
dashboard's builder sends, converted to cron by the server — or a raw
`cron_expr`. `schedule` wins when both are present. Tasks are read back with a
`schedule` derived from the stored expression; anything the builder cannot
express comes back as `{ every: "custom", cron }` and is left alone by an edit.

## Testing

```bash
npm test
```

`node:test`, no extra dependencies. The suite covers the behavior that is easy
to break by accident: the edge-trigger state machine through every delivery
outcome, mutex serialization, run timeouts, spec validation, every comparison
operator, live registry mutation (register / reschedule / unregister), and the
storage guarantees (boot sweep, retention, pagination, delete cascade).

Tests run serially — they share one SQLite database, and concurrent processes
racing to create the schema is a real source of flakes. They use a separate
database file and never touch `data/monitor.db`.

## Notes

- Runs are serialized: Lightpanda's CDP server accepts one connection, one
  context and one page at a time. `noOverlap` stops a task overlapping itself;
  an in-process mutex stops different tasks colliding.
- A run row is written *before* execution, so a crash stays visible instead of
  vanishing. Rows left `running` are marked failed at the next boot.
- Runs older than `RUN_RETENTION_DAYS` are pruned at boot and daily at 04:00.
- `GET /api/runs` returns `condition_met` and `notified` as real booleans; the
  repository converts SQLite's 0/1 at the boundary so the shared types are honest.
