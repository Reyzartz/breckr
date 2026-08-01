# breckr

Self-hosted service that runs browser tasks on a schedule, checks a condition
against what it extracts, alerts you when the condition is met, and serves a
dashboard of run history.

Alerts go to any number of channels — Telegram, Discord, Slack, email, or your
own webhook — created from the dashboard and picked per task.

Tasks are created from the dashboard — no code, no rebuild.

## Quick start (Docker)

The whole thing, one command, no clone:

```bash
docker run -d --name breckr -p 3000:3000 --shm-size=1g \
  -v breckr-data:/app/data \
  -e AUTH_PASSWORD='pick-something-long-and-random' \
  reyzartz/breckr:standalone
```

Open `http://localhost:3000`. `:standalone` bundles headless Chromium, so this
is genuinely the whole story — `--shm-size=1g` is not optional, Chromium
crashes on the 64MB `/dev/shm` a container gets by default. `AUTH_PASSWORD` is
what makes publishing the port past `127.0.0.1` reasonable; see
[Authentication](#authentication).

Prefer Compose, or want the smaller image with Lightpanda instead of a bundled
Chromium?

```bash
curl -O https://raw.githubusercontent.com/Reyzartz/breckr/master/deploy/compose.yaml
AUTH_PASSWORD='pick-something-long-and-random' docker compose up -d
```

See [Images](#images) for what each tag contains, and [Deployment](#deployment)
for running from source instead of the published image.

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
evaluated as code — a task spec cannot become arbitrary code even from an
authenticated session. That is one half of the security story; the other half
is [Authentication](#authentication) itself, which decides who gets a session
in the first place.

Go backend, TypeScript dashboard:

|           |                                                                                      |
| --------- | ------------------------------------------------------------------------------------ |
| `server/` | Go. Scheduler + API + the browser driver. `main.go` → `internal/{app,routes,api,…}`. |
| `client/` | React + Vite on `broke-ui`, routed with TanStack Router and TanStack Query on axios. |

Inside `server/internal`:

|                               |                                                                                                     |
| ----------------------------- | --------------------------------------------------------------------------------------------------- |
| `api`, `routes`, `middleware` | HTTP: chi router, handlers, request logging                                                         |
| `app`                         | wiring and the boot/shutdown sequence                                                               |
| `store`, `migrations`         | SQLite via `database/sql`, schema through goose                                                     |
| `executor`                    | spec validation, the schedule ⇄ cron mapping, extraction and the operator table                     |
| `scheduler`                   | the live cron registry                                                                              |
| `runner`                      | the edge-trigger state machine                                                                      |
| `browser`                     | the CDP connection and the one mutex every run passes through                                       |
| `notifier`                    | a transport per channel kind, and the dispatcher that fans an alert out to all of a task's channels |
| `crypto`                      | AES-GCM at rest for channel credentials                                                             |
| `types`                       | the HTTP contract, mirrored by `client/src/types`                                                   |

Inside `client/src`:

|                          |                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| ------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `routes`                 | one file per page — `/` (dashboard), `/runs` (history), `/channels` — nested under a pathless `_authed` layout that holds the header/nav and the guard described in [Authentication](#authentication); `/login` sits outside it. `@tanstack/router-plugin` generates `routeTree.gen.ts` from this folder; it's gitignored, not checked in.                                                                                                             |
| `hooks`                  | one `use<Resource>` per resource (`useTasks`, `useRuns`, `useHealth`, `useChannels`, `useAuth`), each a thin TanStack Query wrapper: the query plus every mutation on it. `useMonitorEvents`, mounted once in `_authed.tsx`, is what keeps them fresh — no query polls on its own timer; the server pushes a "these resources changed" signal over `/api/events` and this invalidates exactly the query keys it names. Components stay presentational. |
| `services/api`           | one axios-backed `<Resource>Service` class per resource, all extending `ApiClient` in `base.ts`, which unwraps the `{ data }` envelope and turns a failure into an `ApiError` carrying the server's `field`.                                                                                                                                                                                                                                           |
| `components`             | presentational, prop-driven; unchanged in shape by the router migration.                                                                                                                                                                                                                                                                                                                                                                               |
| `constants/queryKeys.ts` | one array root per resource — `[...QueryKeys.runs, filters]` is how a hook narrows to its own cache entry without a naming collision.                                                                                                                                                                                                                                                                                                                  |
| `types`                  | see [The contract on two sides](#the-contract-on-two-sides).                                                                                                                                                                                                                                                                                                                                                                                           |

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

| Route       |                                                                                                                                                                                     |
| ----------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `/`         | Tasks, the warnings that matter before you wait on an alert (browser down, nothing configured to notify, a task with no channels), and the last few runs                            |
| `/runs`     | The full run history — filters and paging are URL search params (`?taskId=…&status=…&offset=…`), so a filtered view is a link you can send someone, not state that resets on reload |
| `/channels` | Create, edit, mute, delete and test delivery destinations                                                                                                                           |

Creating and editing a task stays a modal reachable from `/` rather than its own
route: a spec is complex enough that a full-page context switch would lose the
task list you're authoring it against.

## Tasks

Press **Add task** in the dashboard. A task answers four questions:

|                   |                                                                                                                      |
| ----------------- | -------------------------------------------------------------------------------------------------------------------- |
| **Where**         | a `http`/`https` URL                                                                                                 |
| **What**          | one or more conditions, each a CSS selector plus `text`, `number`, `attribute`, `count` (how many match) or `exists` |
| **When to alert** | per condition, an operator and a value — `is less than 100`, `contains "in stock"`, `changed since the last run`     |
| **What to say**   | an optional message template                                                                                         |

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
described the _old_ condition.

Each task chooses this with **Alert me**, saved as `notify_mode`:

| mode         | behaviour                                                               |
| ------------ | ----------------------------------------------------------------------- |
| `transition` | once on the false → true edge, then quiet until it clears (**default**) |
| `always`     | on every scheduled run where the condition is true                      |

Pick `always` for a task where each matching run is its own event; leave it alone
otherwise, since a repeating alert is the fastest way to stop reading them.

The mode is stored on the task rather than in its spec, so it survives an edit to
the condition, and a `PATCH` carrying only `notify_mode` changes when the task
alerts without touching _whether_ it currently matches. Saving from the dashboard
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

If _every_ channel fails, the alert is still owed: the trigger stays disarmed and
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

## Authentication

Off by default, and that default is deliberate: a local dev instance and a
deployment reachable only from `127.0.0.1` — which is still what
`docker-compose.yml` publishes — have nothing to gain from a login step.

Set `AUTH_PASSWORD` and the dashboard asks for it before showing anything.
This is one shared password, not user accounts — the right fit for a
single-tenant self-hosted service. If you need per-user access or audit trails,
put an identity-aware proxy in front instead; this is not that.

```bash
# docker run
-e AUTH_PASSWORD='pick-something-long-and-random'

# .env
AUTH_PASSWORD=pick-something-long-and-random
```

A correct password sets an `HttpOnly` session cookie, good for
`AUTH_SESSION_TTL_HOURS` (default 30 days). The signing key is derived from the
same master key that already encrypts channel credentials — see
[Notification channels](#notification-channels) — rather than being a second
secret to generate and back up. Because the password is part of that
derivation, **changing `AUTH_PASSWORD` and restarting signs out every session at
once**; there is no per-session revocation otherwise, so that restart is the log
-out-everywhere button.

Five wrong passwords from one address closes a 15-minute window on it. That is
enough to blunt casual guessing on a machine with no proxy in front; a
deployment reachable from the open internet should still rate-limit at the
proxy, since the limiter's per-IP key collapses to one shared bucket behind
one.

`AUTH_COOKIE_SECURE` defaults to `auto`, which sets the cookie's `Secure`
attribute only when the request actually arrived over TLS. This matters because
a hardcoded `Secure` cookie makes login silently impossible over a plain
`http://192.168.1.20:3000` LAN deployment — the browser accepts the response,
drops the cookie, and every request after that 401s with nothing on screen to
explain why. Set it to `true` once you are behind TLS.

What stays reachable without a session: the dashboard's own HTML/JS/CSS (the
login page _is_ this app, so it has to load before anyone can sign in), and
`GET /api/health`, because Docker's `HEALTHCHECK` cannot authenticate — an
anonymous caller gets `{"ok": true}` and nothing else, since the full response
names the browser endpoint and version and counts tasks and channels.
`/api/events`, the dashboard's websocket, needs no special handling: it is
guarded like any other route, and the browser attaches the session cookie to a
same-origin handshake on its own.

### Tasks with no definition

A task can exist with no usable spec — a row written by an older version, or one
whose stored JSON was corrupted. It keeps its run history and is shown as **no
definition**, but cannot be run or edited; only deleted. Such a row is logged at
boot and skipped, deliberately _not_ failing the boot: refusing to start would
lock you out of the only UI that can clean it up.

## Browsers

All three speak CDP, so switching is one line in `.env` — no code changes.

|                         | Lightpanda (default)              | Chrome (fallback)                              | Bundled (`:standalone` image)   |
| ----------------------- | --------------------------------- | ---------------------------------------------- | ------------------------------- |
| Start                   | `docker compose up -d lightpanda` | `docker compose --profile chrome up -d chrome` | nothing — it's inside the image |
| `BROWSER_WS_ENDPOINT`   | `ws://127.0.0.1:9222`             | `http://127.0.0.1:9223`                        | set for you                     |
| Image size              | this app stays ~50MB              | this app stays ~50MB                           | ~600MB, Chromium included       |
| Speed / memory          | very fast, light                  | heavier                                        | heavier                         |
| Screenshots, PDF, WebGL | no — it never renders             | yes                                            | yes                             |
| Web API coverage        | partial (Beta)                    | complete                                       | complete                        |

The first two are what `docker-compose.yml` runs — start the browser separately
from the app, same as local development (`make start-server` on the host).
Bundled is the `breckr:standalone` image: a single `docker run` with no
companion container, at the cost of ~550MB and requiring `--shm-size=1g` (the
default 64MB `/dev/shm` a container gets crashes Chromium on real pages).

Chrome and the bundled Chromium both take an `http://` address because the
browser socket carries a per-launch UUID; the server resolves the real endpoint
itself via `/json/version`.

**If a site returns empty results or throws on Lightpanda, that is the expected
coverage failure** — switch engines and re-run.

> **Security:** an exposed CDP port is remote code execution. Every CDP port in
> this repo's compose files, and the bundled image's internal one, is bound to
> `127.0.0.1` and never published beyond it. Never widen that to `0.0.0.0` to
> fix a connection problem.

## Images

Two tags under [`reyzartz/breckr`](https://hub.docker.com/r/reyzartz/breckr),
both `linux/amd64` and `linux/arm64`:

| tag          | contains                    | size   | use with                                                          |
| ------------ | --------------------------- | ------ | ----------------------------------------------------------------- |
| `latest`     | just the app                | ~50MB  | `docker-compose.yml` / `deploy/compose.yaml`, or your own browser |
| `standalone` | the app + headless Chromium | ~600MB | a single `docker run`, no companion container                     |

Versioned tags follow semver from the git tag: `v1.2.3` publishes `1.2.3`,
`1.2`, `1` and `latest`, and `1.2.3-standalone` … `standalone` alongside them.

## Deployment

Three ways to run this, in increasing order of "how much do I want to build
myself":

**From the published image, Compose (recommended for a server):**

```bash
curl -O https://raw.githubusercontent.com/Reyzartz/breckr/master/deploy/compose.yaml
AUTH_PASSWORD='pick-something-long-and-random' docker compose up -d
```

No clone. Brings Lightpanda with it, publishes to `127.0.0.1` by default — see
`BIND_ADDRESS` in [Configuration](#configuration) before widening that.

**From the published image, `docker run`:** see
[Quick start](#quick-start-docker) for `:standalone`, or for `:latest` with an
external browser:

```bash
docker network create breckr
docker run -d --name lightpanda --network breckr --restart unless-stopped \
  lightpanda/browser:nightly \
  /usr/bin/lightpanda serve --host 0.0.0.0 --port 9222
docker run -d --name breckr --network breckr \
  -p 127.0.0.1:3000:3000 -v breckr-data:/app/data \
  -e BROWSER_WS_ENDPOINT=ws://lightpanda:9222 \
  -e AUTH_PASSWORD='pick-something-long-and-random' \
  --restart unless-stopped reyzartz/breckr:latest
```

Note lightpanda's `9222` is never published — the user-defined network is how
`breckr` reaches it. Do not add `-p 9222:9222`.

**From source, Compose (recommended for development, or if you're modifying
the code):**

```bash
git clone https://github.com/Reyzartz/breckr && cd breckr
cp .env.example .env   # fill in TZ, AUTH_PASSWORD, etc.
docker compose up -d --build
```

`docker compose ps` should show `web-task-monitor` and `lightpanda` running. The
image builds the dashboard and the Go binary in separate stages and ships one
runtime container; the server serves the built dashboard from its own origin, so
the whole thing is one port and nginx is optional. Compose runs exactly one
instance of the app — a second would fight the first over the browser and double
every scheduled run.

Upgrading, published image: `docker compose pull && docker compose up -d`. Note
that `docker-compose.yml` (the from-source file) also carries an `image:` tag,
so a local `--build` shadows a pulled image under that same tag until you pull
again.

Upgrading, from source:

```bash
git pull && docker compose up -d --build
```

Logs: `docker compose logs -f app`. The SQLite database lives in `./data`,
bind-mounted into the container, so it survives `docker compose down` and
image rebuilds — back up `monitor.db` _and_ `secret.key` together, since the
database cannot be read without the key.

By default every container in this repo's compose files runs as root, same as
before. To run as your own user instead:

```bash
sudo chown -R "$(id -u):$(id -g)" ./data   # not chmod -- see below
docker run --user "$(id -u):$(id -g)" -v "$PWD/data:/app/data" ... reyzartz/breckr:latest
```

`chown`, never `chmod`: loosening `data/`'s permissions with `chmod -R` also
loosens `secret.key`, and the server refuses to start with a key file anyone
else on the box can read.

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

All in `.env` (see `.env.example`). `BROWSER_WS_ENDPOINT` (not needed at all on
the `:standalone` image, which sets it for you), `PORT`, `HOST`, `BIND_ADDRESS`
(which host interface Compose publishes the app on — loopback by default;
widen it only alongside `AUTH_PASSWORD`), `DB_PATH`, `TZ` (cron uses wall-clock
time in this zone), `DEFAULT_TIMEOUT_MS`, `RUN_RETENTION_DAYS`, `CLIENT_DIST`
(the dashboard build to serve), `CLIENT_ALLOWED_ORIGIN` (rejected as `*` at boot
once `AUTH_PASSWORD` is set — see [Authentication](#authentication)),
`SECRET_KEY_FILE` (defaults to `secret.key` beside the database),
`AUTH_PASSWORD`, `AUTH_SESSION_TTL_HOURS`, `AUTH_COOKIE_SECURE`. Missing or
contradictory values fail at boot rather than at the first tick.

Notification credentials are deliberately absent: channels are managed from the
dashboard and stored encrypted in the database.

Relative `DB_PATH` and `CLIENT_DIST` resolve against the directory holding the
`.env`, so they work whether the binary runs from the repo root or from
`server/`.

## API

| Route                         |                                                                                                                                           |
| ----------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------- |
| `POST /api/auth/login`        | `{ password }` → sets the session cookie. No session needed to call it.                                                                   |
| `POST /api/auth/logout`       | clears the session cookie. No session needed to call it.                                                                                  |
| `GET /api/auth/status`        | `{ required, authenticated }` — whether this server asks for a password, and whether you have one                                         |
| `GET /api/health`             | liveness always; the rest — whether the browser is reachable, task/channel counts — only with a session, or when `AUTH_PASSWORD` is unset |
| `GET /api/tasks`              | tasks with last run and next run time                                                                                                     |
| `POST /api/tasks`             | create; schedules it immediately                                                                                                          |
| `PATCH /api/tasks/:id`        | any of `{ enabled, name, schedule \| cron_expr, spec, notify_mode, channel_ids }`                                                         |
| `DELETE /api/tasks/:id`       | delete; run history cascades with it                                                                                                      |
| `POST /api/tasks/test`        | run a draft spec once — no run row, no notification                                                                                       |
| `GET /api/runs`               | `task_id`, `status`, `limit` (max 200), `offset`                                                                                          |
| `GET /api/runs/:id`           | full result / error, plus the per-channel `attempts`                                                                                      |
| `POST /api/tasks/:id/run-now` | trigger immediately (works while disabled)                                                                                                |
| `GET /api/channels`           | channels with their secrets masked                                                                                                        |
| `POST /api/channels`          | create `{ name, type, config, enabled? }`                                                                                                 |
| `PATCH /api/channels/:id`     | any of `{ name, config, enabled }`; omitted secrets are kept                                                                              |
| `DELETE /api/channels/:id`    | delete; task links cascade, history is kept                                                                                               |
| `POST /api/channels/test`     | send a test through an unsaved `{ type, config }`                                                                                         |
| `POST /api/channels/:id/test` | send a test through a saved channel                                                                                                       |

Every successful response is `{ "data": … }`. Failures are `{ "error": … }` at
the top level, with a `field` naming the control that was wrong when a spec
fails validation.

Every route below `/api/auth/*` requires a session when `AUTH_PASSWORD` is set
— a request with no valid session cookie gets `401 { "error": "Not signed
in." }`. With no password configured, every request is treated as
authenticated and nothing here changes.

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
- A run row is written _before_ execution, so a crash stays visible instead of
  vanishing. Rows left `running` are marked failed at the next boot.
- Runs older than `RUN_RETENTION_DAYS` are pruned at boot and daily at 04:00.
- `GET /api/runs` returns `condition_met` and `notified` as real booleans; the
  store converts SQLite's 0/1 at the boundary so the contract is honest.
- The schema is applied by goose at boot. A database written by the previous Node
  server migrates in place: every table is created `IF NOT EXISTS`, so the first
  run just stamps the version.
