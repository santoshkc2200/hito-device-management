//go:build integration

package integration

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/hito-hospital/hdms/internal/platform/backup"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
)

func TestHTTPBackupScheduleAndConfig(t *testing.T) {
	h := newTestHarness(t)

	resp := h.get(t, "/v1/backup/config")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("config status = %d", resp.StatusCode)
	}
	cfg := decodeBody[gen.BackupConfig](t, resp)
	if !cfg.Schedule.Enabled || string(cfg.Schedule.Mode) != "daily" || cfg.NextRunAt == nil {
		t.Fatalf("default config = %+v", cfg)
	}
	if cfg.WorkerSeenAt != nil || cfg.LastRun != nil {
		t.Fatalf("fresh database reported worker/run: %+v", cfg)
	}

	resp = h.doJSON(t, http.MethodPut, "/v1/backup/schedule", "", gen.BackupSchedule{
		Enabled: false, Mode: "interval", IntervalMinutes: 60, TimeLocal: "02:00", Weekday: 0,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put schedule status = %d", resp.StatusCode)
	}
	cfg = decodeBody[gen.BackupConfig](t, resp)
	if cfg.Schedule.Enabled || cfg.NextRunAt != nil {
		t.Fatalf("disabled schedule still has a next run: %+v", cfg)
	}

	resp = h.doJSON(t, http.MethodPut, "/v1/backup/schedule", "", gen.BackupSchedule{
		Enabled: true, Mode: "interval", IntervalMinutes: 5, TimeLocal: "02:00",
	})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("invalid schedule status = %d, want 422", resp.StatusCode)
	}

	var audits int
	_ = h.pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_events WHERE action = 'backup.schedule_updated'`).Scan(&audits)
	if audits != 1 {
		t.Fatalf("schedule audit events = %d, want 1", audits)
	}
}

func TestHTTPBackupDestinationsAndRequests(t *testing.T) {
	h := newTestHarness(t)
	target := filepath.Join(os.TempDir(), "hdms-http-test-nas")

	resp := h.post(t, "/v1/backup/destinations", gen.BackupDestinationInput{Name: "NAS", Target: target, RetentionVersions: 2})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", resp.StatusCode)
	}
	d := decodeBody[gen.BackupDestination](t, resp)
	if !d.Enabled {
		t.Fatalf("enabled should default to true: %+v", d)
	}

	resp = h.post(t, "/v1/backup/destinations", gen.BackupDestinationInput{Name: "Etc", Target: "/etc", RetentionVersions: 2})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("outside roots status = %d, want 422", resp.StatusCode)
	}
	resp = h.post(t, "/v1/backup/destinations", gen.BackupDestinationInput{Name: "Dup", Target: target, RetentionVersions: 2})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate status = %d, want 409", resp.StatusCode)
	}

	resp = h.doJSON(t, http.MethodPatch, "/v1/backup/destinations/"+d.Id, "", gen.BackupDestinationUpdate{Name: "NAS A", Enabled: false, RetentionVersions: 4})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch status = %d", resp.StatusCode)
	}

	resp = h.post(t, "/v1/backup/destinations/"+d.Id+"/test", nil)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("test status = %d", resp.StatusCode)
	}
	testReq := decodeBody[gen.BackupRequest](t, resp)
	if string(testReq.Kind) != "test" || string(testReq.Status) != "pending" {
		t.Fatalf("test request = %+v", testReq)
	}

	first := decodeBody[gen.BackupRequest](t, h.post(t, "/v1/backup/run", nil))
	second := decodeBody[gen.BackupRequest](t, h.post(t, "/v1/backup/run", nil))
	if first.Id != second.Id {
		t.Fatalf("double click queued two runs: %s %s", first.Id, second.Id)
	}

	resp = h.get(t, "/v1/backup/requests/"+first.Id)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("poll status = %d", resp.StatusCode)
	}
	resp = h.get(t, "/v1/backup/requests/00000000-0000-0000-0000-000000000000")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing request status = %d, want 404", resp.StatusCode)
	}

	resp = h.post(t, "/v1/backup/verify", gen.VerifyBackupsRequest{})
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("verify status = %d", resp.StatusCode)
	}

	if err := backup.ReplaceSnapshots(context.Background(), h.pool.Pool, backup.LocalRepoKey, []backup.Snapshot{{ID: "abc"}}, testReq.RequestedAt); err != nil {
		t.Fatal(err)
	}
	snaps := decodeBody[gen.BackupSnapshotList](t, h.get(t, "/v1/backup/snapshots?repo=local"))
	if len(snaps.Items) != 1 || snaps.Items[0].SnapshotId != "abc" {
		t.Fatalf("snapshots = %+v", snaps)
	}
	resp = h.get(t, "/v1/backup/snapshots?repo=not-a-uuid")
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("bad repo status = %d, want 422", resp.StatusCode)
	}

	resp = h.doJSON(t, http.MethodDelete, "/v1/backup/destinations/"+d.Id, "", nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d", resp.StatusCode)
	}
	list := decodeBody[gen.BackupDestinationList](t, h.get(t, "/v1/backup/destinations"))
	if len(list.Items) != 0 {
		t.Fatalf("destinations after delete = %+v", list.Items)
	}

	runs := decodeBody[gen.BackupRunList](t, h.get(t, "/v1/backup/runs?limit=5"))
	if runs.Items == nil {
		t.Fatal("runs.items must be an empty array, not null")
	}
}
