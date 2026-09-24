//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/hito-hospital/hdms/internal/platform/backup"
	backupstore "github.com/hito-hospital/hdms/internal/platform/backup/store"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/ids"
	"github.com/hito-hospital/hdms/test/testdb"
)

func TestBackupDestinationsRoundTrip(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)

	q := backupstore.New(db.Conn(ctx, pool))

	created, err := q.CreateDestination(ctx, backupstore.CreateDestinationParams{
		ID:                ids.NewUUID(),
		Name:              "NAS",
		Kind:              "path",
		Target:            "/mnt/nas-backups",
		Provider:          "lan",
		Enabled:           true,
		RetentionVersions: 2,
		UpdatedBy:         "test",
	})
	if err != nil {
		t.Fatalf("CreateDestination: %v", err)
	}
	if created.RetentionVersions != 2 {
		t.Fatalf("RetentionVersions = %d, want 2", created.RetentionVersions)
	}

	enabled, err := q.ListEnabledDestinations(ctx)
	if err != nil {
		t.Fatalf("ListEnabledDestinations: %v", err)
	}
	if len(enabled) != 1 {
		t.Fatalf("ListEnabledDestinations returned %d rows, want 1", len(enabled))
	}

	loaded, err := backup.LoadEnabledDestinations(ctx, pool)
	if err != nil {
		t.Fatalf("LoadEnabledDestinations: %v", err)
	}
	if len(loaded) != 1 || loaded[0].Name != "NAS" || loaded[0].RetentionVersions != 2 {
		t.Fatalf("LoadEnabledDestinations = %+v, want one NAS destination with K=2", loaded)
	}
}

func TestBackupDestinationsRejectDuplicateTarget(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)

	q := backupstore.New(db.Conn(ctx, pool))
	params := backupstore.CreateDestinationParams{
		ID: ids.NewUUID(), Name: "one", Kind: "rclone",
		Target: "gdrive:hdms", Provider: "google_drive",
		Enabled: true, RetentionVersions: 2, UpdatedBy: "test",
	}
	if _, err := q.CreateDestination(ctx, params); err != nil {
		t.Fatalf("first CreateDestination: %v", err)
	}
	params.ID = ids.NewUUID()
	params.Name = "two"
	if _, err := q.CreateDestination(ctx, params); err == nil {
		t.Fatal("second CreateDestination with the same target = nil error, want unique violation")
	}
}

func TestBackupDestinationsRejectRetentionBelowOne(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)

	q := backupstore.New(db.Conn(ctx, pool))
	if _, err := q.CreateDestination(ctx, backupstore.CreateDestinationParams{
		ID: ids.NewUUID(), Name: "bad", Kind: "path",
		Target: "/mnt/x", Provider: "lan",
		Enabled: true, RetentionVersions: 0, UpdatedBy: "test",
	}); err == nil {
		t.Fatal("RetentionVersions = 0 accepted, want check violation")
	}
}
