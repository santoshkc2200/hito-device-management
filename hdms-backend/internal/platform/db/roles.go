package db

import (
	"context"
	"errors"
	"fmt"
)

// ProvisionAppRole gives the restricted runtime role hdms_app a login and the
// given password. Migration 0019 creates the role NOLOGIN because a migration
// must not carry a password; the worker calls this at start, as the database
// owner, so a production install needs no hand-run SQL.
//
// ALTER ROLE cannot take a bind parameter, so the statement is built by
// Postgres's own format('%L') quoting rather than by string concatenation.
func ProvisionAppRole(ctx context.Context, q DBTX, password string) error {
	if password == "" {
		return errors.New("db: provision hdms_app: HDMS_APP_DB_PASSWORD is empty")
	}
	var stmt string
	if err := q.QueryRow(ctx,
		`SELECT format('ALTER ROLE hdms_app WITH LOGIN PASSWORD %L', $1::text)`, password,
	).Scan(&stmt); err != nil {
		return fmt.Errorf("db: provision hdms_app: build statement: %w", err)
	}
	if _, err := q.Exec(ctx, stmt); err != nil {
		return fmt.Errorf("db: provision hdms_app: %w", err)
	}
	return nil
}
