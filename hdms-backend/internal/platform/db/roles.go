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

// GrantAppPrivileges gives hdms_app the privileges migration 0019 grants. A
// restore needs it: pg_restore runs with --no-privileges, so a restored
// database otherwise grants hdms_app nothing and the production API, which
// connects as hdms_app, can read no table. Keep in step with 0019.
func GrantAppPrivileges(ctx context.Context, q DBTX) error {
	for _, stmt := range []string{
		`DO $$
		BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hdms_app') THEN
				CREATE ROLE hdms_app NOLOGIN;
			END IF;
		END
		$$`,
		`GRANT USAGE ON SCHEMA public TO hdms_app`,
		`GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO hdms_app`,
		`REVOKE ALL ON TABLE audit_events FROM PUBLIC`,
		`REVOKE UPDATE, DELETE, TRUNCATE ON TABLE audit_events FROM hdms_app`,
		`GRANT SELECT, INSERT ON TABLE audit_events TO hdms_app`,
		`GRANT USAGE, SELECT, UPDATE ON ALL SEQUENCES IN SCHEMA public TO hdms_app`,
		`ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO hdms_app`,
		`ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT, UPDATE ON SEQUENCES TO hdms_app`,
	} {
		if _, err := q.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("db: grant hdms_app privileges: %w", err)
		}
	}
	return nil
}
