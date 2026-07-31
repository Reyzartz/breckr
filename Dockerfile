# syntax=docker/dockerfile:1

# One image, one process: the Go binary serves the API, owns the cron registry,
# drives the browser over CDP, and serves the dashboard build from the same
# origin and port. There is no second container for the client because there is
# nothing for it to do -- and no CORS to configure as a result.
#
# Two targets come out of this file:
#
#   --target runtime      reyzartz/breckr:latest      ~35MB, brings its own
#                                                     browser from elsewhere
#   --target standalone   reyzartz/breckr:standalone  ~600MB, Chromium inside,
#                                                     so one `docker run` works
#
# Pass --target explicitly, always. `standalone` derives from `runtime` and is
# therefore the last stage, so a bare `docker build .` builds the large one.

# ---- client: build the dashboard -------------------------------------------
#
# Pinned to BUILDPLATFORM because the output is static JS and CSS, which is the
# same bytes on every architecture. Without this the whole npm install and Vite
# build would run under QEMU on the arm64 leg of a multi-arch build, turning
# two minutes into twenty for no difference in the result.
FROM --platform=$BUILDPLATFORM node:22-alpine AS client-build
WORKDIR /build

# brake-ui is a git dependency, and its lockfile entry resolves over ssh.
# `npm ci` honours `resolved` verbatim, so without this rewrite the install
# depends on npm quietly falling back to the HTTPS tarball -- which works today
# and is not something to build a release pipeline on.
RUN apk add --no-cache git \
 && git config --global url."https://github.com/".insteadOf "ssh://git@github.com/"

# Only the manifests, so this layer is reused until a dependency actually
# changes -- editing source does not reinstall node_modules. No glob on the
# lockfile: `npm ci` requires one, and a missing file should fail here with a
# clear message rather than three lines later with an obscure one.
COPY client/package.json client/package-lock.json ./
RUN npm ci --no-audit --no-fund

COPY client/ ./
RUN npm run build

# ---- server: cross-compile a static binary ---------------------------------
#
# Also BUILDPLATFORM: CGO_ENABLED=0 Go cross-compiles natively, so the target
# architecture is an argument rather than a reason to emulate anything.
FROM --platform=$BUILDPLATFORM golang:1.25.7-alpine AS server-build
WORKDIR /build

COPY server/go.mod server/go.sum ./
RUN go mod download

COPY server/ ./
ARG TARGETOS TARGETARCH
# CGO_ENABLED=0 because the SQLite driver is pure Go (modernc.org/sqlite). That
# is the whole reason the runtime image can be bare alpine -- the previous Node
# image needed Debian trixie purely to satisfy better-sqlite3's prebuilt binary.
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o breckr-server ./main.go

# ---- runtime: the slim image ------------------------------------------------
FROM alpine:3.22 AS runtime
WORKDIR /app

# ca-certificates is needed to reach the notification transports over HTTPS and
# SMTP over STARTTLS. tzdata is needed for
# time.LoadLocation to resolve IANA zones like America/New_York -- alpine ships
# neither by default. The healthcheck's wget comes from busybox, which is
# already there.
RUN apk add --no-cache ca-certificates tzdata

COPY --from=server-build /build/breckr-server ./breckr-server
COPY --from=client-build /build/dist ./client/dist

# HOST is what makes `docker run -p 3000:3000` work at all. The server's own
# default is 127.0.0.1, which inside a container binds the container's loopback
# and makes a correctly published port look dead.
#
# Binding every interface is not the security boundary here and is not meant to
# be: the boundary is which address you publish the port on, and whether
# AUTH_PASSWORD is set. See the README.
ENV HOST=0.0.0.0 \
    PORT=3000 \
    CLIENT_DIST=/app/client/dist \
    DB_PATH=/app/data/monitor.db

# Created before VOLUME, which freezes the directory against anything a later
# layer does to it.
RUN mkdir -p /app/data
# The database, its WAL sidecars, and secret.key -- which the database is
# useless without. Anyone who forgets -v still keeps their data across a
# `docker rm`, at the cost of an anonymous volume they have to find later.
VOLUME ["/app/data"]

EXPOSE 3000

# Shell form so PORT is read at run time rather than baked in at build time.
# busybox wget has no HEAD, so --spider issues a GET and discards the body.
# /api/health answers unauthenticated -- Docker cannot log in -- but tells an
# anonymous caller only that the process is alive.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -q --spider "http://127.0.0.1:${PORT}/api/health" || exit 1

LABEL org.opencontainers.image.title="breckr" \
      org.opencontainers.image.description="Self-hosted browser task monitor: scheduled checks, conditions and alerts" \
      org.opencontainers.image.source="https://github.com/Reyzartz/breckr" \
      org.opencontainers.image.url="https://github.com/Reyzartz/breckr" \
      org.opencontainers.image.licenses="MIT"

CMD ["./breckr-server"]

# ---- standalone: the same thing with a browser inside -----------------------
#
# For `docker run` with no companion container. Compose users want the slim
# image and Lightpanda, which is both smaller and faster.
FROM runtime AS standalone

# chromium needs the font and NSS packages to render at all. tini is PID 1: it
# reaps the browser's children and forwards SIGTERM, neither of which a bare
# `sh` does.
RUN apk add --no-cache chromium tini nss freetype harfbuzz ttf-freefont

COPY docker/standalone-entrypoint.sh /usr/local/bin/standalone-entrypoint.sh
RUN chmod +x /usr/local/bin/standalone-entrypoint.sh

# http:// rather than ws://, because Chrome's browser-level socket carries a
# per-launch UUID in its path. config.go sets NeedsResolve for an http(s)
# endpoint and finds the real socket itself via /json/version, which is the
# only way to point at a Chrome that restarts.
ENV BROWSER_WS_ENDPOINT=http://127.0.0.1:9222 \
    CHROMIUM_FLAGS=""

LABEL org.opencontainers.image.description="Self-hosted browser task monitor, with headless Chromium bundled"

ENTRYPOINT ["/sbin/tini", "--", "/usr/local/bin/standalone-entrypoint.sh"]
# Re-declared deliberately: setting ENTRYPOINT in a derived stage resets a CMD
# inherited from the base, and without this the entrypoint would exec nothing.
CMD ["./breckr-server"]
