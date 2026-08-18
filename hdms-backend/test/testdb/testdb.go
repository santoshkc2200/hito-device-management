// Package testdb gives integration tests a real, migrated Postgres without
// paying the container-startup or migration cost per test. One container
// and one migrated template database are shared for the whole test binary;
// each call to New clones the template into a fresh database (a Postgres
// `CREATE DATABASE ... TEMPLATE`, which is a filesystem copy, not a re-run
// of every migration) and drops it when the test finishes.
package testdb

import (
	"context"
	"fmt"
	"net/url"
	"sync"
	"testing"

	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/jackc/pgx/v5"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

const templateDBName = "hdms_template"

var (
	setupOnce sync.Once
	setupErr  error
	adminDSN  string // connects to the container with no database selected (defaults to "postgres")
	cloneSeq  int
	cloneMu   sync.Mutex
)

// New returns a pool connected to a freshly cloned, fully migrated
// database, and registers cleanup to drop it when t ends. The underlying
// container is started at most once per test binary run and left alive for
// the rest of the run — starting it per test is what makes testcontainers
// slow enough that people stop running the suite (see the Phase 0 risk
// register).
func New(t *testing.T) *db.Pool {
	t.Helper()
	ctx := context.Background()

	setupOnce.Do(func() { setupErr = setup(ctx) })
	if setupErr != nil {
		t.Fatalf("testdb: container setup: %v", setupErr)
	}

	cloneName := nextCloneName()
	if err := cloneTemplate(ctx, cloneName); err != nil {
		t.Fatalf("testdb: clone template: %v", err)
	}
	t.Cleanup(func() {
		if err := dropDatabase(context.Background(), cloneName); err != nil {
			t.Logf("testdb: drop %s: %v", cloneName, err)
		}
	})

	pool, err := db.Open(ctx, dsnFor(cloneName))
	if err != nil {
		t.Fatalf("testdb: open clone pool: %v", err)
	}
	t.Cleanup(pool.Close)

	return pool
}

func setup(ctx context.Context) error {
	pgContainer, err := tcpostgres.Run(ctx, "postgres:18-alpine",
		tcpostgres.WithDatabase("postgres"),
		tcpostgres.WithUsername("hdms"),
		tcpostgres.WithPassword("hdms"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		return fmt.Errorf("start postgres container: %w", err)
	}

	base, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return fmt.Errorf("connection string: %w", err)
	}
	adminDSN = base

	if err := createDatabase(ctx, templateDBName); err != nil {
		return fmt.Errorf("create template database: %w", err)
	}
	if err := db.Migrate(ctx, dsnFor(templateDBName)); err != nil {
		return fmt.Errorf("migrate template database: %w", err)
	}
	return nil
}

func nextCloneName() string {
	cloneMu.Lock()
	defer cloneMu.Unlock()
	cloneSeq++
	return fmt.Sprintf("hdms_test_%d", cloneSeq)
}

func createDatabase(ctx context.Context, name string) error {
	conn, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		return err
	}
	defer conn.Close(ctx) //nolint:errcheck
	_, err = conn.Exec(ctx, fmt.Sprintf(`CREATE DATABASE %s`, pgx.Identifier{name}.Sanitize()))
	return err
}

func cloneTemplate(ctx context.Context, name string) error {
	conn, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		return err
	}
	defer conn.Close(ctx) //nolint:errcheck
	_, err = conn.Exec(ctx, fmt.Sprintf(
		`CREATE DATABASE %s TEMPLATE %s`,
		pgx.Identifier{name}.Sanitize(), pgx.Identifier{templateDBName}.Sanitize(),
	))
	return err
}

func dropDatabase(ctx context.Context, name string) error {
	conn, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		return err
	}
	defer conn.Close(ctx) //nolint:errcheck
	_, err = conn.Exec(ctx, fmt.Sprintf(
		`DROP DATABASE IF EXISTS %s WITH (FORCE)`, pgx.Identifier{name}.Sanitize(),
	))
	return err
}

// dsnFor rewrites adminDSN to point at a different database on the same
// container.
func dsnFor(dbName string) string {
	u, err := url.Parse(adminDSN)
	if err != nil {
		// adminDSN was already validated in setup; this can't happen.
		panic(err)
	}
	u.Path = "/" + dbName
	return u.String()
}
