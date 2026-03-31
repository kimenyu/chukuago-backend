build:
	@go build -o bin/chukuagobackend cmd/main.go

run:
	@./bin/chukuagobackend

dev:
	@go run cmd/main.go

# Run migrations against Neon
migrate:
	@psql $(postgresql://neondb_owner:npg_zI8rfp6YmclE@ep-billowing-darkness-anea1o98-pooler.c-6.us-east-1.aws.neon.tech/neondb?sslmode=require) -f cmd/migrate/migrations/001_init_schema.sql


migrate-up:
	@migrate -path cmd/migrate/migrations -database "$(postgresql://neondb_owner:npg_zI8rfp6YmclE@ep-billowing-darkness-anea1o98-pooler.c-6.us-east-1.aws.neon.tech/neondb?sslmode=require)" up

migrate-down:
	@migrate -path cmd/migrate/migrations -database "$(postgresql://neondb_owner:npg_zI8rfp6YmclE@ep-billowing-darkness-anea1o98-pooler.c-6.us-east-1.aws.neon.tech/neondb?sslmode=require)" down

migrate-force:
	@migrate -path cmd/migrate/migrations -database "$(postgresql://neondb_owner:npg_zI8rfp6YmclE@ep-billowing-darkness-anea1o98-pooler.c-6.us-east-1.aws.neon.tech/neondb?sslmode=require)" force 1