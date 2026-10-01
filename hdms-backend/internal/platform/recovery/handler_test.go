package recovery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/backup"
)

var runningSecrets = backup.RecoverySecrets{
	BackupEncKey: "YmFja3VwLWtleS1iYWNrdXAta2V5LWJhY2t1cC1rZXk=", TokenPepper: "pepper",
	CredentialEncKey: "Y3JlZC1rZXktY3JlZC1rZXktY3JlZC1rZXktY3JlZC0=", TOTPEncKey: "dG90cC1rZXktdG90cC1rZXktdG90cC1rZXktdG90cC0=",
}

type handlerFixture struct {
	h      *Handler
	srv    http.Handler
	engine *Engine
	ops    *fakeOps
	key    backup.RecoveryKey
	now    *time.Time
	logs   *bytes.Buffer
}

// newHandlerFixture writes a bundle sealed for sealed into a local source and
// serves the handler with runningSecrets as the worker's own.
func newHandlerFixture(t *testing.T, sealed backup.RecoverySecrets) *handlerFixture {
	t.Helper()
	folder := t.TempDir()
	key, _ := backup.NewRecoveryKey()
	bundle, err := backup.SealRecoveryBundle(key, sealed, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, backup.RecoveryBundleFile), bundle, 0o600); err != nil {
		t.Fatal(err)
	}
	e, ops := newTestEngine(t)
	now := testNow
	clock := func() time.Time { return now }
	logs := &bytes.Buffer{}
	h := &Handler{
		Engine: e,
		Status: func(context.Context) LiveState { return LiveDamaged },
		Worker: func() string { return "database_unavailable" },
		Sources: func(context.Context) []Source {
			return []Source{{ID: "local", Kind: SourceLocal, Folder: folder, HasKey: true}}
		},
		Snapshots: func(context.Context, Source) ([]backup.Snapshot, error) {
			return []backup.Snapshot{
				{ID: "old", Time: testTaken.Add(-24 * time.Hour)},
				{ID: "snap1", Time: testTaken, Summary: &backup.SnapshotStats{TotalBytesProcessed: 4096}},
			}, nil
		},
		Secrets: runningSecrets,
		Limiter: &Limiter{PerIP: 5, Total: 20, Now: clock},
		Now:     clock,
		Logger:  slog.New(slog.NewTextHandler(logs, nil)),
	}
	return &handlerFixture{h: h, srv: h.Routes(), engine: e, ops: ops, key: key, now: &now, logs: logs}
}

func (f *handlerFixture) do(method, path, body, ip string, cookie *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("X-Forwarded-For", ip)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	f.srv.ServeHTTP(rec, req)
	return rec
}

func (f *handlerFixture) unlock(t *testing.T, key, ip string) (*httptest.ResponseRecorder, *http.Cookie) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"source": "local", "key": key})
	rec := f.do(http.MethodPost, "/recovery/api/unlock", string(body), ip, nil)
	for _, c := range rec.Result().Cookies() {
		if c.Name == "hdms_recovery" {
			return rec, c
		}
	}
	return rec, nil
}

func errorCodeOf(rec *httptest.ResponseRecorder) string {
	var body struct{ Error string }
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return body.Error
}

func TestStatusNeedsNoSession(t *testing.T) {
	f := newHandlerFixture(t, runningSecrets)
	rec := f.do(http.MethodGet, "/recovery/api/status", "", "10.0.0.5", nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status = %d cache %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
	var body struct {
		Database       string
		Worker         string
		RestoreRunning bool
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Database != "damaged" || body.Worker != "database_unavailable" || body.RestoreRunning {
		t.Fatalf("status body = %+v", body)
	}
}

func TestSourcesNeedNoSession(t *testing.T) {
	f := newHandlerFixture(t, runningSecrets)
	rec := f.do(http.MethodGet, "/recovery/api/sources", "", "10.0.0.5", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"hasRecoveryKey":true`) {
		t.Fatalf("sources = %d %s", rec.Code, rec.Body.String())
	}
}

func TestUnlockIssuesAStrictSessionCookie(t *testing.T) {
	f := newHandlerFixture(t, runningSecrets)
	rec, c := f.unlock(t, strings.ToLower(f.key.String()), "10.0.0.5")
	if rec.Code != http.StatusOK || c == nil {
		t.Fatalf("unlock = %d %s", rec.Code, rec.Body.String())
	}
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode || c.Path != "/recovery" || len(c.Value) < 40 {
		t.Fatalf("cookie = %+v", c)
	}
	if strings.Contains(f.logs.String(), f.key.String()) || strings.Contains(strings.ToUpper(f.logs.String()), strings.ReplaceAll(f.key.String(), "-", "")) {
		t.Fatal("the recovery key reached the log")
	}
}

func TestUnlockTyposDoNotCount(t *testing.T) {
	f := newHandlerFixture(t, runningSecrets)
	s := f.key.String()
	last := s[len(s)-1]
	swapped := byte('0')
	if last == '0' {
		swapped = '1'
	}
	typo := s[:len(s)-1] + string(swapped)
	for i := 0; i < 8; i++ {
		rec, _ := f.unlock(t, typo, "10.0.0.5")
		if rec.Code != http.StatusUnprocessableEntity || errorCodeOf(rec) != "key_typo" {
			t.Fatalf("typo %d = %d %s, want 422 key_typo", i, rec.Code, errorCodeOf(rec))
		}
	}
	if rec, _ := f.unlock(t, "not a key", "10.0.0.5"); errorCodeOf(rec) != "key_format" {
		t.Fatalf("garbage = %s, want key_format", errorCodeOf(rec))
	}
	if rec, _ := f.unlock(t, s, "10.0.0.5"); rec.Code != http.StatusOK {
		t.Fatalf("correct key after typos = %d; typos must not use up attempts", rec.Code)
	}
}

func TestUnlockRateLimits(t *testing.T) {
	f := newHandlerFixture(t, runningSecrets)
	wrong, _ := backup.NewRecoveryKey()
	for i := 0; i < 5; i++ {
		if rec, _ := f.unlock(t, wrong.String(), "10.0.0.5"); rec.Code != http.StatusUnauthorized || errorCodeOf(rec) != "key_wrong" {
			t.Fatalf("wrong key %d = %d %s", i, rec.Code, errorCodeOf(rec))
		}
	}
	rec, _ := f.unlock(t, f.key.String(), "10.0.0.5")
	if rec.Code != http.StatusTooManyRequests || errorCodeOf(rec) != "too_many_attempts" || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("6th attempt = %d %s Retry-After %q", rec.Code, errorCodeOf(rec), rec.Header().Get("Retry-After"))
	}
	// 5 so far; 15 more from fresh IPs reach the hourly total of 20.
	for i := 0; i < 15; i++ {
		ip := "10.1.0." + string(rune('a'+i))
		if rec, _ := f.unlock(t, wrong.String(), ip); rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt from %s = %d", ip, rec.Code)
		}
	}
	if rec, _ := f.unlock(t, f.key.String(), "10.9.9.9"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("21st attempt in the hour from a fresh IP = %d, want 429", rec.Code)
	}
	if !strings.Contains(f.logs.String(), "wrong_key") {
		t.Fatal("failed attempts must be logged with their outcome")
	}
}

func TestUnlockUnknownSourceAndMissingBundle(t *testing.T) {
	f := newHandlerFixture(t, runningSecrets)
	body, _ := json.Marshal(map[string]string{"source": "path:/elsewhere", "key": f.key.String()})
	if rec := f.do(http.MethodPost, "/recovery/api/unlock", string(body), "10.0.0.5", nil); rec.Code != http.StatusNotFound || errorCodeOf(rec) != "source_not_found" {
		t.Fatalf("unknown source = %d %s", rec.Code, errorCodeOf(rec))
	}
	empty := t.TempDir()
	f.h.Sources = func(context.Context) []Source { return []Source{{ID: "local", Kind: SourceLocal, Folder: empty}} }
	if rec, _ := f.unlock(t, f.key.String(), "10.0.0.5"); rec.Code != http.StatusNotFound || errorCodeOf(rec) != "no_bundle" {
		t.Fatalf("no bundle = %d %s", rec.Code, errorCodeOf(rec))
	}
}

func TestKeysMismatchIssuesASessionButRefusesRestore(t *testing.T) {
	other := runningSecrets
	other.TokenPepper = "a different pepper"
	f := newHandlerFixture(t, other)
	rec, c := f.unlock(t, f.key.String(), "10.0.0.5")
	if rec.Code != http.StatusConflict || errorCodeOf(rec) != "keys_mismatch" || c == nil {
		t.Fatalf("unlock = %d %s cookie %v, want 409 keys_mismatch with a session", rec.Code, errorCodeOf(rec), c)
	}
	if rec := f.do(http.MethodGet, "/recovery/api/snapshots", "", "10.0.0.5", c); errorCodeOf(rec) != "keys_mismatch" {
		t.Fatalf("snapshots = %d %s", rec.Code, errorCodeOf(rec))
	}
	if rec := f.do(http.MethodPost, "/recovery/api/restore", `{"snapshotId":"snap1","confirmation":"RESTORE"}`, "10.0.0.5", c); rec.Code != http.StatusConflict || errorCodeOf(rec) != "keys_mismatch" {
		t.Fatalf("restore = %d %s", rec.Code, errorCodeOf(rec))
	}
	if len(f.ops.callLog()) != 0 {
		t.Fatal("a mismatched restore reached the engine")
	}
}

func TestSessionIsRequiredAndExpires(t *testing.T) {
	f := newHandlerFixture(t, runningSecrets)
	if rec := f.do(http.MethodGet, "/recovery/api/snapshots", "", "10.0.0.5", nil); rec.Code != http.StatusUnauthorized || errorCodeOf(rec) != "session_required" {
		t.Fatalf("no cookie = %d %s", rec.Code, errorCodeOf(rec))
	}
	forged := &http.Cookie{Name: "hdms_recovery", Value: "forged"}
	if rec := f.do(http.MethodGet, "/recovery/api/snapshots", "", "10.0.0.5", forged); rec.Code != http.StatusUnauthorized {
		t.Fatalf("forged cookie = %d", rec.Code)
	}
	_, c := f.unlock(t, f.key.String(), "10.0.0.5")
	*f.now = f.now.Add(29 * time.Minute)
	if rec := f.do(http.MethodGet, "/recovery/api/snapshots", "", "10.0.0.5", c); rec.Code != http.StatusOK {
		t.Fatalf("after 29 minutes = %d", rec.Code)
	}
	*f.now = f.now.Add(29 * time.Minute) // 29 minutes since last use
	if rec := f.do(http.MethodGet, "/recovery/api/snapshots", "", "10.0.0.5", c); rec.Code != http.StatusOK {
		t.Fatalf("29 minutes after last use = %d; the lifetime runs from last use", rec.Code)
	}
	*f.now = f.now.Add(31 * time.Minute)
	if rec := f.do(http.MethodGet, "/recovery/api/snapshots", "", "10.0.0.5", c); rec.Code != http.StatusUnauthorized {
		t.Fatalf("31 minutes idle = %d, want 401", rec.Code)
	}
}

func TestSnapshotsNewestFirst(t *testing.T) {
	f := newHandlerFixture(t, runningSecrets)
	_, c := f.unlock(t, f.key.String(), "10.0.0.5")
	rec := f.do(http.MethodGet, "/recovery/api/snapshots", "", "10.0.0.5", c)
	var body struct {
		Snapshots []struct {
			ID        string
			SizeBytes int64
		}
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if len(body.Snapshots) != 2 || body.Snapshots[0].ID != "snap1" || body.Snapshots[0].SizeBytes != 4096 {
		t.Fatalf("snapshots = %s", rec.Body.String())
	}
	f.h.Snapshots = func(context.Context, Source) ([]backup.Snapshot, error) { return nil, errors.New("repository locked") }
	if rec := f.do(http.MethodGet, "/recovery/api/snapshots", "", "10.0.0.5", c); rec.Code != http.StatusBadGateway || errorCodeOf(rec) != "repository_unreadable" {
		t.Fatalf("unreadable repository = %d %s", rec.Code, errorCodeOf(rec))
	}
}

func TestRestoreFlow(t *testing.T) {
	f := newHandlerFixture(t, runningSecrets)
	f.ops.live = LiveDamaged
	_, c := f.unlock(t, f.key.String(), "10.0.0.5")

	if rec := f.do(http.MethodPost, "/recovery/api/restore", `{"snapshotId":"snap1","confirmation":"restore"}`, "10.0.0.5", c); rec.Code != http.StatusUnprocessableEntity || errorCodeOf(rec) != "confirmation_required" {
		t.Fatalf("lower-case confirmation = %d %s", rec.Code, errorCodeOf(rec))
	}
	if rec := f.do(http.MethodPost, "/recovery/api/restore", `{"snapshotId":"nope","confirmation":"RESTORE"}`, "10.0.0.5", c); rec.Code != http.StatusNotFound || errorCodeOf(rec) != "snapshot_not_found" {
		t.Fatalf("unknown snapshot = %d %s", rec.Code, errorCodeOf(rec))
	}

	f.ops.block = make(chan struct{})
	rec := f.do(http.MethodPost, "/recovery/api/restore", `{"snapshotId":"snap1","confirmation":"RESTORE"}`, "10.0.0.5", c)
	if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), `"phase":"running"`) {
		t.Fatalf("restore = %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "hdms_restore") || strings.Contains(rec.Body.String(), "10.0.0.5") {
		t.Fatal("the response leaks database names or the IP")
	}
	if rec := f.do(http.MethodPost, "/recovery/api/restore", `{"snapshotId":"snap1","confirmation":"RESTORE"}`, "10.0.0.5", c); rec.Code != http.StatusConflict || errorCodeOf(rec) != "restore_running" {
		t.Fatalf("second restore = %d %s", rec.Code, errorCodeOf(rec))
	}
	if rec := f.do(http.MethodGet, "/recovery/api/status", "", "10.0.0.5", nil); !strings.Contains(rec.Body.String(), `"restoreRunning":true`) {
		t.Fatalf("status during a restore = %s", rec.Body.String())
	}
	close(f.ops.block)
	f.engine.Wait()

	rec = f.do(http.MethodGet, "/recovery/api/restore", "", "10.0.0.5", c)
	if !strings.Contains(rec.Body.String(), `"phase":"completed"`) || !strings.Contains(rec.Body.String(), `"canUndo":false`) {
		t.Fatalf("restore state = %s (live was damaged, so no undo)", rec.Body.String())
	}
	st, _ := f.engine.State()
	if st.UnlockIP != "10.0.0.5" || !st.SnapshotTakenAt.Equal(testTaken) {
		t.Fatalf("engine got %+v", st)
	}
	if rec := f.do(http.MethodPost, "/recovery/api/restore/undo", `{"confirmation":"RESTORE"}`, "10.0.0.5", c); rec.Code != http.StatusConflict || errorCodeOf(rec) != "nothing_to_undo" {
		t.Fatalf("undo = %d %s", rec.Code, errorCodeOf(rec))
	}
}

func TestUndoFlow(t *testing.T) {
	f := newHandlerFixture(t, runningSecrets)
	f.ops.live = LiveWorking
	_, c := f.unlock(t, f.key.String(), "10.0.0.5")
	f.do(http.MethodPost, "/recovery/api/restore", `{"snapshotId":"snap1","confirmation":"RESTORE"}`, "10.0.0.5", c)
	f.engine.Wait()
	if rec := f.do(http.MethodPost, "/recovery/api/restore/undo", `{"confirmation":"yes"}`, "10.0.0.5", c); errorCodeOf(rec) != "confirmation_required" {
		t.Fatalf("undo without RESTORE = %s", errorCodeOf(rec))
	}
	rec := f.do(http.MethodPost, "/recovery/api/restore/undo", `{"confirmation":"RESTORE"}`, "10.0.0.5", c)
	if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), `"kind":"undo"`) {
		t.Fatalf("undo = %d %s", rec.Code, rec.Body.String())
	}
	f.engine.Wait()
}

func TestRestoreWhenTheServerIsDown(t *testing.T) {
	f := newHandlerFixture(t, runningSecrets)
	f.ops.live = LiveServerDown
	_, c := f.unlock(t, f.key.String(), "10.0.0.5")
	if rec := f.do(http.MethodPost, "/recovery/api/restore", `{"snapshotId":"snap1","confirmation":"RESTORE"}`, "10.0.0.5", c); rec.Code != http.StatusConflict || errorCodeOf(rec) != "database_server_down" {
		t.Fatalf("restore = %d %s", rec.Code, errorCodeOf(rec))
	}
}
