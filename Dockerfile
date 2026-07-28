# syntax=docker/dockerfile:1

# One image, one process: the Go binary serves the API, owns the cron registry,
# drives the browser over CDP, and serves the dashboard build from the same
# origin and port. There is no second container for the client because there is
# nothing for it to do -- and no CORS to configure as a result.

# ---- client: build the dashboard ------------------------------------------
FROM node:22-alpine AS client
WORKDIR /build

# Only the manifests, so this layer is reused until a dependency actually
# changes -- editing source does not reinstall node_modules.
COPY client/package.json client/package-lock.json* ./
RUN npm install --no-audit --no-fund

COPY client/ ./
RUN npm run build

# ---- server: build a static binary ----------------------------------------
FROM golang:1.25-alpine AS server
WORKDIR /build

COPY server/go.mod server/go.sum ./
RUN go mod download

COPY server/ ./
# CGO_ENABLED=0 because the SQLite driver is pure Go (modernc.org/sqlite). That
# is the whole reason the runtime image can be bare alpine -- the previous Node
# image needed Debian trixie purely to satisfy better-sqlite3's prebuilt binary.
RUN CGO_ENABLED=0 GOOS=linux go build -o breckr-server ./main.go

# ---- runtime ---------------------------------------------------------------
FROM alpine:3.22
WORKDIR /app

# ca-certificates is needed to reach Telegram over HTTPS. The healthcheck's wget
# comes from busybox, which is already there.
RUN apk add --no-cache ca-certificates

COPY --from=server /build/breckr-server ./breckr-server
COPY --from=client /build/dist ./client/dist

ENV CLIENT_DIST=/app/client/dist

# Cosmetic -- what actually gets published is set in docker-compose.yml, which
# reads PORT from .env.
EXPOSE 3000

CMD ["./breckr-server"]
