package database

import (
	"context"
	"fmt"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPostgres creates a pgxpool connection pool tuned for Neon's serverless Postgres.
//
// Neon specifics:
//   - SSL is required: the DSN must contain sslmode=require (Neon rejects plain connections).
//   - Neon "scales to zero" — cold starts add ~500 ms on first connection. The pool's
//     MinConns=0 default is correct here; holding idle connections against a sleeping
//     compute wastes your free-tier compute hours.
//   - For high-RPS endpoints, enable Neon's built-in PgBouncer and use the pooled
//     connection string instead. PgBouncer multiplexes many app connections onto a
//     small number of real Postgres connections, which Neon's serverless architecture
//     handles much more efficiently.
func NewPostgres(dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse postgres dsn: %w", err)
	}

	// Keep pool small — Neon charges per compute second and connection overhead
	// on serverless Postgres is higher than on a dedicated instance.
	cfg.MaxConns = 10
	cfg.MinConns = 0 // let Neon's compute sleep when idle
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute  // release idle conns quickly
	cfg.HealthCheckPeriod = 30 * time.Second

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	return pool, nil
}

// RunMigrations applies pending database migrations from the given directory.
// golang-migrate reads the DSN directly, so SSL params in the DSN are respected.
func RunMigrations(dsn, migrationsPath string) error {
	m, err := migrate.New(fmt.Sprintf("file://%s", migrationsPath), dsn)
	if err != nil {
		return fmt.Errorf("init migrator: %w", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("apply migrations: %w", err)
	}

	return nil
}
