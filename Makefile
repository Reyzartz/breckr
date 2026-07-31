ifneq (,$(wildcard ./.env))
	include .env
	export
endif

IMAGE ?= reyzartz/breckr

.PHONY: start start-server start-client stop build build-server build-client test typecheck check \
	docker-up docker-down docker-build docker-build-multiarch docker-run-standalone

# Start the server (API + scheduler + browser driver, one process)
start-server:
	cd server && go run main.go

# Start the dashboard's dev server; Vite proxies /api to the Go server
start-client:
	cd client && npm run dev

# Start both together
start:
	make start-server & make start-client

stop:
	pkill -f "go run main.go" || true
	pkill -f "breckr-server" || true
	pkill -f "vite" || true

# --- build -----------------------------------------------------------------

build: build-client build-server

build-server:
	cd server && CGO_ENABLED=0 go build -o breckr-server ./main.go

build-client:
	cd client && npm run build

# --- checks ----------------------------------------------------------------

test:
	cd server && go test ./...

typecheck:
	cd client && npm run typecheck
	cd server && go vet ./...

# What CI gates a release on. Run this before tagging.
check: test typecheck

# --- docker ----------------------------------------------------------------

docker-up:
	docker compose up -d --build

docker-down:
	docker compose down

# Both images for this machine's architecture. --target is never optional:
# `standalone` is the last stage, so a bare `docker build .` builds the big one.
docker-build:
	docker build --target runtime    -t $(IMAGE):latest     .
	docker build --target standalone -t $(IMAGE):standalone .

# What CI builds, without pushing. Proves `apk add chromium` resolves on arm64
# and that the cross-compile args are right. Neither --push nor --load, so
# buildx builds both and discards the result.
docker-build-multiarch:
	docker buildx build --platform linux/amd64,linux/arm64 --target runtime    .
	docker buildx build --platform linux/amd64,linux/arm64 --target standalone .

# The one-container path, the way the README documents it. --shm-size is not
# optional: Chromium crashes on the 64MB /dev/shm `docker run` gives it.
docker-run-standalone:
	docker run --rm -p 127.0.0.1:3000:3000 --shm-size=1g \
	  -v breckr-data:/app/data \
	  -e AUTH_PASSWORD=$(AUTH_PASSWORD) \
	  $(IMAGE):standalone
