package db

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"

	"github.com/hito-hospital/hdms/migrations"
)

// ErrSchemaNotCurrent means the database answers but does not carry exactly
// the migrations this build embeds: never migrated, half migrated, or
// restored from an older snapshot and not yet caught up.
var ErrSchemaNotCurrent = errors.New("db: schema version does not match this build")

// LatestMigration is the highest migration version embedded in the binary.
func LatestMigration() (int64, error) {
	names, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		return 0, fmt.Errorf("db: list embedded migrations: %w", err)
	}
	var latest int64
	for _, n := range names {
		prefix, _, ok := strings.Cut(n, "_")
		if !ok {
			continue
		}
		v, err := strconv.ParseInt(prefix, 10, 64)
		if err != nil {
			continue
		}
		latest = max(latest, v)
	}
	if latest == 0 {
		return 0, errors.New("db: no migrations embedded")
	}
	return latest, nil
}

// SchemaVersion is the highest applied goose version in the database. A
// database goose never touched has no version table, which is an error.
func SchemaVersion(ctx context.Context, q DBTX) (int64, error) {
	var v *int64
	if err := q.QueryRow(ctx, `SELECT max(version_id) FROM goose_db_version WHERE is_applied`).Scan(&v); err != nil {
		return 0, fmt.Errorf("db: read schema version: %w", err)
	}
	if v == nil {
		return 0, nil
	}
	return *v, nil
}

// SchemaCurrent reports whether the database is at exactly this build's
// latest migration. /v1/readyz and the recovery engine both use it.
func SchemaCurrent(ctx context.Context, q DBTX) error {
	want, err := LatestMigration()
	if err != nil {
		return err
	}
	got, err := SchemaVersion(ctx, q)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrSchemaNotCurrent, err)
	}
	if got != want {
		return fmt.Errorf("%w: database at %d, build expects %d", ErrSchemaNotCurrent, got, want)
	}
	return nil
}
