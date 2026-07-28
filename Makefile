ifneq (,$(wildcard ./.env))
	include .env
	export
endif

.PHONY: start start-server start-client stop build build-server build-client test typecheck docker-up docker-down

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

# --- docker ----------------------------------------------------------------

docker-up:
	docker compose up -d --build

docker-down:
	docker compose down
