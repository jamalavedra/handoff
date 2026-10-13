GO ?= go
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: build web test vet check dev-db dev

build: web
	$(GO) build -ldflags "-X main.version=$(VERSION)" -o bin/handoff ./cmd/handoff

web:
	cd web && npm ci && npm run build

test:
	$(GO) test ./...

vet:
	gofmt -l . | (! grep .) && $(GO) vet ./...

check: vet test
	cd web && npm run typecheck

# Disposable Postgres for local development and tests (port 55432).
dev-db:
	docker run -d --name handoff-pg -e POSTGRES_PASSWORD=pg -p 55432:5432 postgres:17-alpine

dev:
	DATABASE_URL=postgres://postgres:pg@localhost:55432/postgres?sslmode=disable HANDOFF_DEV=1 $(GO) run ./cmd/handoff
