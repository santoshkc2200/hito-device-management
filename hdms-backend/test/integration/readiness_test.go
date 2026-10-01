//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hito-hospital/hdms/internal/apiserver"
	"github.com/hito-hospital/hdms/internal/platform/backup"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/test/testdb"
)

func readyz(t *testing.T, pool *db.Pool) *httptest.ResponseRecorder {
	t.Helper()
	srv := apiserver.New(pool, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, "test", apiserver.BackupConsoleConfig{})
	rec := httptest.NewRecorder()
	srv.GetReadyz(rec, httptest.NewRequest(http.MethodGet, "/v1/readyz", nil))
	return rec
}

func readyzStatus(t *testing.T, pool *db.Pool) int {
	t.Helper()
	return readyz(t, pool).Code
}

// The API starts with the database down and becomes ready once a migrated
// database answers. "Answers" alone is not ready: an empty or half-restored
// database would fail every query the clients make.
func TestReadyzFollowsTheDatabase(t *testing.T) {
	down, err := db.OpenLazy("postgres://hdms:hdms@127.0.0.1:1/hdms?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatalf("OpenLazy must not connect: %v", err)
	}
	defer down.Close()
	if got := readyzStatus(t, down); got != http.StatusServiceUnavailable {
		t.Fatalf("readyz with the database down = %d, want 503", got)
	}

	scratchURL := testdb.Scratch(t)
	empty, err := db.OpenLazy(scratchURL)
	if err != nil {
		t.Fatal(err)
	}
	defer empty.Close()
	if got := readyzStatus(t, empty); got != http.StatusServiceUnavailable {
		t.Fatalf("readyz on an unmigrated database = %d, want 503", got)
	}

	if err := db.Migrate(context.Background(), scratchURL); err != nil {
		t.Fatal(err)
	}
	if got := readyzStatus(t, empty); got != http.StatusOK {
		t.Fatalf("readyz once migrated = %d, want 200", got)
	}
}

func TestSchemaCurrentRejectsAnOlderVersion(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	if err := db.SchemaCurrent(ctx, pool.Pool); err != nil {
		t.Fatalf("freshly migrated clone: %v", err)
	}
	latest, _ := db.LatestMigration()
	if _, err := pool.Exec(ctx, `DELETE FROM goose_db_version WHERE version_id = $1`, latest); err != nil {
		t.Fatal(err)
	}
	if err := db.SchemaCurrent(ctx, pool.Pool); err == nil {
		t.Fatal("SchemaCurrent accepted a database one migration behind")
	}
}

// Every client showing the maintenance notice asks /v1/readyz every 15
// seconds; it must say "maintenance" for as long as a restore holds the
// switch, and 200 the moment it lets go.
func TestReadyzReportsMaintenance(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	if got := readyzStatus(t, pool); got != http.StatusOK {
		t.Fatalf("readyz before maintenance = %d, want 200", got)
	}

	if err := backup.SetMaintenance(ctx, pool.Pool, true, "restore"); err != nil {
		t.Fatal(err)
	}
	rec := readyz(t, pool)
	var problem struct{ Type string }
	_ = json.Unmarshal(rec.Body.Bytes(), &problem)
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") != "15" || !strings.HasSuffix(problem.Type, "/maintenance") {
		t.Fatalf("readyz during maintenance = %d, type %q, Retry-After %q; want 503, …/maintenance, 15",
			rec.Code, problem.Type, rec.Header().Get("Retry-After"))
	}

	if err := backup.SetMaintenance(ctx, pool.Pool, false, ""); err != nil {
		t.Fatal(err)
	}
	if got := readyzStatus(t, pool); got != http.StatusOK {
		t.Fatalf("readyz after maintenance = %d, want 200", got)
	}
}
