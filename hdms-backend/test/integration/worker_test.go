//go:build integration

package integration

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/jobs"
	"github.com/hito-hospital/hdms/test/testdb"
)

func TestLastStartedReturnsNewestStartPerJob(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	base := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	for _, r := range []struct {
		job string
		at  time.Time
	}{
		{"backup", base.Add(1 * time.Hour)},
		{"backup", base.Add(5 * time.Hour)},
		{"overdue-scan", base.Add(2 * time.Hour)},
		{"retention", base.Add(9 * time.Hour)},
	} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO job_runs (job, started_at, finished_at, outcome) VALUES ($1, $2, $2, 'failure')`,
			r.job, r.at); err != nil {
			t.Fatalf("insert job_runs: %v", err)
		}
	}

	got, err := jobs.LastStarted(ctx, pool.Pool, []string{"backup", "overdue-scan", "weekly-digest"})
	if err != nil {
		t.Fatalf("LastStarted: %v", err)
	}
	if !got["backup"].Equal(base.Add(5 * time.Hour)) {
		t.Errorf("backup = %s, want newest start", got["backup"])
	}
	if !got["overdue-scan"].Equal(base.Add(2 * time.Hour)) {
		t.Errorf("overdue-scan = %s", got["overdue-scan"])
	}
	if _, ok := got["weekly-digest"]; ok {
		t.Errorf("weekly-digest present with no rows")
	}
	if _, ok := got["retention"]; ok {
		t.Errorf("retention returned although not requested")
	}
}

// The password is applied to the cluster-wide hdms_app role; other tests
// connect as the owner, so changing it does not disturb them.
func TestProvisionAppRoleQuotesPassword(t *testing.T) {
	pool, dsn := testdb.NewWithDSN(t)
	ctx := context.Background()
	password := `it's a "test" \ pass`

	if err := db.ProvisionAppRole(ctx, pool.Pool, password); err != nil {
		t.Fatalf("ProvisionAppRole: %v", err)
	}

	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	u.User = url.UserPassword("hdms_app", password)
	conn, err := pgx.Connect(ctx, u.String())
	if err != nil {
		t.Fatalf("connect as hdms_app with provisioned password: %v", err)
	}
	defer conn.Close(ctx)

	if err := db.VerifyPrivileges(ctx, conn); err != nil {
		t.Fatalf("hdms_app must pass the production privilege check: %v", err)
	}
}

func TestProvisionAppRoleRefusesEmptyPassword(t *testing.T) {
	pool := testdb.New(t)
	if err := db.ProvisionAppRole(context.Background(), pool.Pool, ""); err == nil {
		t.Fatal("empty password accepted")
	}
}
