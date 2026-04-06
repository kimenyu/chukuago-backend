BINARY      = chukuago-api
MAIN        = ./cmd/server
MIGRATE_URL = $(DATABASE_URL)

.PHONY: build run test lint migrate-up migrate-down migrate-create seed tidy

build:
	go build -ldflags="-s -w" -o bin/$(BINARY) $(MAIN)

run:
	go run $(MAIN)/main.go

test:
	go test ./... -race -cover

lint:
	golangci-lint run ./...

tidy:
	go mod tidy

# ── Database migrations ──────────────────────────────────────────────────────

migrate-up:
	migrate -path ./migrations -database "$(DATABASE_URL)" up

migrate-down:
	migrate -path ./migrations -database "$(DATABASE_URL)" down 1

migrate-down-all:
	migrate -path ./migrations -database "$(DATABASE_URL)" down

migrate-create:
	@read -p "Migration name: " name; \
	migrate create -ext sql -dir ./migrations -seq $$name

migrate-status:
	migrate -path ./migrations -database "$(DATABASE_URL)" version

# ── Docker helpers ───────────────────────────────────────────────────────────

docker-up:
	docker compose up -d postgres redis

docker-down:
	docker compose down

docker-logs:
	docker compose logs -f

# ── Code generation ──────────────────────────────────────────────────────────

generate:
	go generate ./...
