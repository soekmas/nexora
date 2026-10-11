.PHONY: help up down logs test test-backend test-frontend fmt lint lint-backend lint-frontend build build-api build-worker dev-api dev-worker dev-pos

help:
	@echo "NEXORA Phase 0 Commands:"
	@echo "  make up             - Start PostgreSQL and Redis in background"
	@echo "  make down           - Stop local Docker containers"
	@echo "  make logs           - Stream container logs"
	@echo "  make test           - Run backend and frontend tests"
	@echo "  make fmt            - Format Go and frontend source code"
	@echo "  make lint           - Lint backend and frontend code"
	@echo "  make build          - Compile API and Worker binaries"
	@echo "  make dev-api        - Run API service locally"
	@echo "  make dev-worker     - Run Worker daemon locally"
	@echo "  make dev-pos        - Run POS Web Next.js dev server"

up:
	docker compose up -d

down:
	docker compose down

logs:
	docker compose logs -f

test-backend:
	go test -race -v ./backend/... ./apps/api/... ./apps/worker/...

test-frontend:
	pnpm --filter @nexora/pos-web test

test: test-backend test-frontend

fmt:
	gofmt -w ./backend ./apps/api ./apps/worker
	pnpm prettier --write "apps/pos-web/src/**/*.{ts,tsx,css}" 2>/dev/null || true

lint-backend:
	golangci-lint run ./...

lint-frontend:
	pnpm --filter @nexora/pos-web lint

lint: lint-backend lint-frontend

build-api:
	mkdir -p bin
	go build -o bin/api ./apps/api/cmd/api

build-worker:
	mkdir -p bin
	go build -o bin/worker ./apps/worker/cmd/worker

build: build-api build-worker

dev-api:
	go run ./apps/api/cmd/api

dev-worker:
	go run ./apps/worker/cmd/worker

dev-pos:
	pnpm --filter @nexora/pos-web dev
