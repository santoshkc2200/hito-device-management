//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
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
	target := filepath.Join(t.TempDir(), "nas")
	if err := os.Mkdir(target, 0o750); err != nil {
		t.Fatal(err)
	}

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

func TestHTTPBackupLocations(t *testing.T) {
	h := newTestHarness(t)
	drive := filepath.Join(t.TempDir(), "drive")
	if err := os.Mkdir(drive, 0o750); err != nil {
		t.Fatal(err)
	}

	listing := decodeBody[gen.BackupLocationListing](t, h.get(t, "/v1/backup/locations?path="+url.QueryEscape(filepath.Dir(drive))))
	if len(listing.Folders) != 1 || listing.Folders[0].Name != "drive" {
		t.Fatalf("listing = %+v", listing)
	}

	resp := h.post(t, "/v1/backup/locations/folders", gen.BackupFolderInput{Parent: drive, Name: "hdms-backups"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create folder status = %d", resp.StatusCode)
	}
	folder := decodeBody[gen.BackupFolder](t, resp)
	if resp := h.post(t, "/v1/backup/locations/folders", gen.BackupFolderInput{Parent: drive, Name: "hdms-backups"}); resp.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate folder status = %d, want 409", resp.StatusCode)
	}
	if resp := h.post(t, "/v1/backup/locations/folders", gen.BackupFolderInput{Parent: drive, Name: "../escape"}); resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("traversal name status = %d, want 422", resp.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(drive), "escape")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("traversal name created a folder")
	}
	if resp := h.get(t, "/v1/backup/locations?path=/etc"); resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("browse /etc status = %d, want 422", resp.StatusCode)
	}

	check := decodeBody[gen.BackupLocationCheck](t, h.post(t, "/v1/backup/locations/check", gen.BackupLocationCheckInput{Path: folder.Path}))
	if !check.Ok || len(check.Checks) != 7 {
		t.Fatalf("check = %+v", check)
	}
	bad := decodeBody[gen.BackupLocationCheck](t, h.post(t, "/v1/backup/locations/check", gen.BackupLocationCheckInput{Path: "/etc"}))
	if bad.Ok || string(bad.Checks[0].Status) != "fail" || bad.Checks[0].Code == nil || string(*bad.Checks[0].Code) != "outside_roots" {
		t.Fatalf("check /etc = %+v", bad)
	}

	// The server re-checks on create: a folder with other files is refused.
	busy := filepath.Join(drive, "busy")
	if err := os.MkdirAll(busy, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(busy, "notes.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if resp := h.post(t, "/v1/backup/destinations", gen.BackupDestinationInput{Name: "Busy", Target: busy, RetentionVersions: 3}); resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("busy folder create status = %d, want 422", resp.StatusCode)
	}

	// A folder already holding HDMS backups is reused, not refused.
	reuse := filepath.Join(drive, "reuse")
	if err := os.MkdirAll(filepath.Join(reuse, "repo"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(reuse, "repo", "config"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if resp := h.post(t, "/v1/backup/destinations", gen.BackupDestinationInput{Name: "Reuse", Target: reuse, RetentionVersions: 3}); resp.StatusCode != http.StatusCreated {
		t.Fatalf("existing-repo create status = %d, want 201", resp.StatusCode)
	}

	var audits int
	_ = h.pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_events WHERE action = 'backup.location_folder_created'`).Scan(&audits)
	if audits != 1 {
		t.Fatalf("folder audit events = %d, want 1", audits)
	}
}

func TestHTTPBackupLocationsWorkerDown(t *testing.T) {
	h := newTestHarness(t)
	target := filepath.Join(t.TempDir(), "nas")
	if err := os.Mkdir(target, 0o750); err != nil {
		t.Fatal(err)
	}
	h.workerServer.Close()

	resp := h.get(t, "/v1/backup/locations")
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("browse status = %d, want 503", resp.StatusCode)
	}
	var problem struct {
		Type string `json:"type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&problem); err != nil || !strings.HasSuffix(problem.Type, "worker-unavailable") {
		t.Fatalf("problem = %+v, %v", problem, err)
	}
	if resp := h.post(t, "/v1/backup/destinations", gen.BackupDestinationInput{Name: "NAS", Target: target, RetentionVersions: 3}); resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("create with worker down status = %d, want 503 (fail closed)", resp.StatusCode)
	}
}
