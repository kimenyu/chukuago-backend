build:
	@go build -o bin/chukuagobackend cmd/main.go

run:
	@./bin/chukuagobackend

dev:
	@go run cmd/main.go

# Run migrations against Neon
migrate:
	@psql $(DATABASE_URL) -f cmd/migrate/migrations/001_init_schema.sql