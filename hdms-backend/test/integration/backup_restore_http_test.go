//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/hito-hospital/hdms/internal/platform/backup"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
)

const runningView = `{"restore":{"kind":"restore","phase":"running","step":"safety_backup",
	"steps":["safety_backup","restore_scratch","migrate_scratch","validate","maintenance_on","copy_forward","swap","maintenance_off","record"],
	"sourceKind":"local","snapshotTakenAt":"2026-09-29T02:00:00Z","startedAt":"2026-10-01T09:00:00Z","canUndo":false,"canDiscard":false}}`

const completedView = `{"restore":{"kind":"restore","phase":"completed","step":"record","steps":["record"],
	"sourceKind":"local","snapshotTakenAt":"2026-09-29T02:00:00Z","startedAt":"2026-10-01T09:00:00Z",
	"finishedAt":"2026-10-01T09:05:00Z","canUndo":true,"canDiscard":true}}`

func problemType(t *testing.T, resp *http.Response) string {
	t.Helper()
	var p struct {
		Type string `json:"type"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&p)
	return p.Type[strings.LastIndex(p.Type, "/")+1:]
}

func (h *testHarness) totpCode(t *testing.T) string {
	t.Helper()
	c, err := totp.GenerateCode(h.adminTOTPSecret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func (h *testHarness) adminActor(t *testing.T) string {
	t.Helper()
	var id string
	if err := h.pool.QueryRow(context.Background(), `SELECT id::text FROM admin_accounts WHERE email = 'admin@example.org'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return "admin:" + id
}

func auditCount(t *testing.T, h *testHarness, action string) int {
	t.Helper()
	var n int
	_ = h.pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_events WHERE action = $1`, action).Scan(&n)
	return n
}

func TestHTTPBackupRestore(t *testing.T) {
	h := newTestHarness(t)
	w := h.restoreWorker

	got := decodeBody[gen.BackupRestoreStatus](t, h.get(t, "/v1/backup/restore"))
	if got.Restore != nil || got.Maintenance || !got.WorkerAvailable {
		t.Fatalf("initial status = %+v", got)
	}

	input := func(word, password string) gen.BackupRestoreInput {
		return gen.BackupRestoreInput{Repo: "local", SnapshotId: "snap1", Confirmation: gen.BackupRestoreInputConfirmation(word), Password: password, TotpCode: h.totpCode(t)}
	}
	before := len(w.callLog())

	// The confirmation word is checked before anything else: with a wrong
	// password too, the answer is confirmation-required, not reauth-failed,
	// so a missing word never counts toward the lockout.
	resp := h.post(t, "/v1/backup/restore", input("restore", "wrong password here"))
	if resp.StatusCode != http.StatusUnprocessableEntity || problemType(t, resp) != "confirmation-required" {
		t.Fatalf("lowercase word: %d", resp.StatusCode)
	}
	// A wrong password is 422, never 401, and the worker is not asked.
	resp = h.post(t, "/v1/backup/restore", input("RESTORE", "wrong password here"))
	if resp.StatusCode != http.StatusUnprocessableEntity || problemType(t, resp) != "reauth-failed" {
		t.Fatalf("wrong password: %d", resp.StatusCode)
	}
	if n := len(w.callLog()); n != before {
		t.Fatalf("worker called %d times before re-authentication passed", n-before)
	}

	w.answer(http.StatusAccepted, runningView)
	resp = h.post(t, "/v1/backup/restore", input("RESTORE", h.adminPassword))
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("start status = %d", resp.StatusCode)
	}
	started := decodeBody[gen.BackupRestoreStatus](t, resp)
	if started.Restore == nil || started.Restore.Phase != "running" {
		t.Fatalf("start body = %+v", started)
	}
	calls := w.callLog()
	last := calls[len(calls)-1]
	if !strings.HasPrefix(last, "POST /internal/restore ") ||
		!strings.Contains(last, `"repo":"local"`) || !strings.Contains(last, `"snapshotId":"snap1"`) ||
		!strings.Contains(last, `"requestedBy":"`+h.adminActor(t)+`"`) {
		t.Fatalf("worker call = %s", last)
	}
	if n := auditCount(t, h, "backup.restore.requested"); n != 1 {
		t.Fatalf("requested audit events = %d", n)
	}

	// A second restore while one runs.
	w.answer(http.StatusConflict, `{"error":"restore_running"}`)
	resp = h.post(t, "/v1/backup/restore", input("RESTORE", h.adminPassword))
	if resp.StatusCode != http.StatusConflict || problemType(t, resp) != "restore-running" {
		t.Fatalf("second start: %d", resp.StatusCode)
	}

	// Ending maintenance is refused while the worker reports a running restore.
	if _, err := h.pool.Exec(context.Background(), `UPDATE system_state SET maintenance = true, maintenance_reason = 'restore'`); err != nil {
		t.Fatal(err)
	}
	w.answer(http.StatusOK, runningView)
	resp = h.post(t, "/v1/backup/maintenance/end", nil)
	if resp.StatusCode != http.StatusConflict || problemType(t, resp) != "restore-running" {
		t.Fatalf("end maintenance while running: %d", resp.StatusCode)
	}

	w.answer(http.StatusOK, completedView)
	resp = h.post(t, "/v1/backup/maintenance/end", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("end maintenance: %d", resp.StatusCode)
	}
	if st := decodeBody[gen.BackupRestoreStatus](t, resp); st.Maintenance {
		t.Fatal("maintenance still on")
	}
	if m, _ := backup.GetMaintenance(context.Background(), h.pool.Pool); m.On {
		t.Fatal("maintenance still on in the database")
	}
	if n := auditCount(t, h, "backup.maintenance.ended"); n != 1 {
		t.Fatalf("maintenance audit events = %d", n)
	}

	// Undo needs the word and the password too.
	resp = h.post(t, "/v1/backup/restore/undo", gen.BackupRestoreConfirm{Confirmation: "RESTORE", Password: "wrong password here", TotpCode: h.totpCode(t)})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("undo wrong password: %d", resp.StatusCode)
	}
	w.answer(http.StatusAccepted, runningView)
	resp = h.post(t, "/v1/backup/restore/undo", gen.BackupRestoreConfirm{Confirmation: "RESTORE", Password: h.adminPassword, TotpCode: h.totpCode(t)})
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("undo status = %d", resp.StatusCode)
	}
	calls = w.callLog()
	if last := calls[len(calls)-1]; !strings.HasPrefix(last, "POST /internal/restore/undo ") {
		t.Fatalf("undo worker call = %s", last)
	}
	if n := auditCount(t, h, "backup.restore.undo_requested"); n != 1 {
		t.Fatalf("undo audit events = %d", n)
	}

	w.answer(http.StatusConflict, `{"error":"nothing_to_discard"}`)
	resp = h.post(t, "/v1/backup/restore/discard", nil)
	if resp.StatusCode != http.StatusConflict || problemType(t, resp) != "nothing-to-discard" {
		t.Fatalf("discard with nothing kept: %d", resp.StatusCode)
	}
	w.answer(http.StatusOK, completedView)
	resp = h.post(t, "/v1/backup/restore/discard", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("discard status = %d", resp.StatusCode)
	}
	if n := auditCount(t, h, "backup.restore.discarded"); n != 1 {
		t.Fatalf("discard audit events = %d", n)
	}

	// A cloud destination is not a restore source yet.
	w.answer(http.StatusUnprocessableEntity, `{"error":"source_unsupported"}`)
	resp = h.post(t, "/v1/backup/restore", input("RESTORE", h.adminPassword))
	if resp.StatusCode != http.StatusUnprocessableEntity || problemType(t, resp) != "restore-source-unsupported" {
		t.Fatalf("cloud source: %d", resp.StatusCode)
	}
}

func TestHTTPBackupRestoreWorkerDown(t *testing.T) {
	h := newTestHarness(t)
	if _, err := h.pool.Exec(context.Background(), `UPDATE system_state SET maintenance = true, maintenance_reason = 'restore'`); err != nil {
		t.Fatal(err)
	}
	h.workerServer.Close()

	got := decodeBody[gen.BackupRestoreStatus](t, h.get(t, "/v1/backup/restore"))
	if got.WorkerAvailable || !got.Maintenance || got.Restore != nil {
		t.Fatalf("status with the worker down = %+v", got)
	}
	resp := h.post(t, "/v1/backup/restore", gen.BackupRestoreInput{
		Repo: "local", SnapshotId: "snap1", Confirmation: "RESTORE", Password: h.adminPassword, TotpCode: h.totpCode(t)})
	if resp.StatusCode != http.StatusServiceUnavailable || problemType(t, resp) != "worker-unavailable" {
		t.Fatalf("start with the worker down: %d", resp.StatusCode)
	}
	// The emergency exit still works: a dead worker is not running a restore.
	resp = h.post(t, "/v1/backup/maintenance/end", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("end maintenance with the worker down: %d", resp.StatusCode)
	}
	if m, _ := backup.GetMaintenance(context.Background(), h.pool.Pool); m.On {
		t.Fatal("maintenance still on")
	}
}
