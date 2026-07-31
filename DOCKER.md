# breckr

Self-hosted service that runs browser tasks on a schedule, checks a condition
against what it extracts, and alerts you — Telegram, Discord, Slack, email, or
your own webhook — when it's met. Tasks are created from a dashboard, no code
and no rebuild. Full docs, source and issue tracker:
[github.com/Reyzartz/breckr](https://github.com/Reyzartz/breckr).

## Two tags

| tag | contains | size | use with |
|---|---|---|---|
| `latest` | just the app | ~50MB | Compose, or your own CDP browser |
| `standalone` | the app + headless Chromium | ~600MB | a single `docker run`, nothing else |

Both are `linux/amd64` and `linux/arm64`. Versioned tags follow semver from the
release: `1.2.3`, `1.2`, `1`, and the `-standalone` equivalents.

## `docker run` — the one-command path

```bash
docker run -d --name breckr -p 3000:3000 --shm-size=1g \
  -v breckr-data:/app/data \
  -e AUTH_PASSWORD='pick-something-long-and-random' \
  reyzartz/breckr:standalone
```

Open `http://localhost:3000`. `--shm-size=1g` is not optional — the 64MB
`/dev/shm` a container gets by default crashes Chromium on real pages.
`AUTH_PASSWORD` is what makes publishing this port past `127.0.0.1` reasonable
— see [Auth](#auth) below.

`:latest` needs a browser next door instead:

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

Never publish lightpanda's `9222` — an open CDP port is remote code execution.
The two containers reach each other over the user-defined network instead.

## `docker compose`

```bash
curl -O https://raw.githubusercontent.com/Reyzartz/breckr/master/deploy/compose.yaml
AUTH_PASSWORD='pick-something-long-and-random' docker compose up -d
```

Brings Lightpanda with it and publishes to `127.0.0.1` by default — set
`BIND_ADDRESS=0.0.0.0` in the environment to widen that, alongside
`AUTH_PASSWORD`.

## Environment

| var | default | |
|---|---|---|
| `AUTH_PASSWORD` | unset (off) | one shared password for the dashboard; ≥8 chars |
| `BROWSER_WS_ENDPOINT` | set by `:standalone`; required otherwise | CDP address |
| `TZ` | `UTC` | cron matches wall-clock time in this zone |
| `PORT` | `3000` | |
| `BIND_ADDRESS` | `127.0.0.1` | Compose only — which host interface to publish on |
| `AUTH_SESSION_TTL_HOURS` | `720` (30 days) | |
| `AUTH_COOKIE_SECURE` | `auto` | `auto` \| `true` \| `false` — see Auth |
| `RUN_RETENTION_DAYS` | `30` | |

Full list, including what each does and why:
[README → Configuration](https://github.com/Reyzartz/breckr#configuration).

## Volumes, ports, healthcheck

- `/app/data` — SQLite (`monitor.db` + WAL sidecars) and `secret.key`, the
  AES-256 key channel credentials are encrypted with. **Back the two up
  together** — the database is unreadable without the key. A named volume
  (`-v breckr-data:/app/data`) needs no host directory or ownership setup; a
  bind mount works too.
- `3000/tcp` — the dashboard and API, one origin.
- `HEALTHCHECK` is built in (`GET /api/health`, every 30s).

## Auth

Unset `AUTH_PASSWORD` and there is no login page at all — right for local use
and for a deployment that never leaves `127.0.0.1`. Set it and a session cookie
guards everything except the dashboard's own assets and `/api/health` (which
tells an anonymous caller only that the process is alive — Docker's healthcheck
can't sign in). This is one shared password, not user accounts; put an
identity-aware proxy in front if you need per-user access. Changing
`AUTH_PASSWORD` and restarting signs every session out at once — that's the
only revocation a stateless cookie gets, and it's there on purpose. Full
details: [README → Authentication](https://github.com/Reyzartz/breckr#authentication).

## Upgrading

```bash
docker pull reyzartz/breckr:standalone   # or :latest
docker rm -f breckr
docker run … reyzartz/breckr:standalone  # same command as before
```

The volume carries `monitor.db` and `secret.key` across. With Compose:
`docker compose pull && docker compose up -d`.

---

Issues, source, and the full README:
[github.com/Reyzartz/breckr](https://github.com/Reyzartz/breckr)
