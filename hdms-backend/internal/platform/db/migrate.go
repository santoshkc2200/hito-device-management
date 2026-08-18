package db

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/hito-hospital/hdms/migrations"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// migrationsAdvisoryLockID is an arbitrary, fixed lock key. Every instance
// racing to start (e.g. two replicas deployed at once) blocks on this lock
// rather than running goose concurrently against the same schema.
const migrationsAdvisoryLockID = 8_942_017

// Migrate applies every pending migration, embedded in the binary so that
// deploying is just "run the binary" — no separate migration step or file
// to ship alongside it. It serializes concurrent starts with a Postgres
// advisory lock.
func Migrate(ctx context.Context, databaseURL string) error {
	sqlDB, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("db: migrate: open: %w", err)
	}
	defer sqlDB.Close() //nolint:errcheck

	if _, err := sqlDB.ExecContext(ctx, "SELECT pg_advisory_lock($1)", migrationsAdvisoryLockID); err != nil {
		return fmt.Errorf("db: migrate: acquire advisory lock: %w", err)
	}
	defer sqlDB.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", migrationsAdvisoryLockID) //nolint:errcheck

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("db: migrate: set dialect: %w", err)
	}

	if err := goose.UpContext(ctx, sqlDB, "."); err != nil {
		return fmt.Errorf("db: migrate: up: %w", err)
	}
	return nil
}
