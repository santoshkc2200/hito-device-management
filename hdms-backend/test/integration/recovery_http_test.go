//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/backup"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/recovery"
	"github.com/hito-hospital/hdms/test/testdb"
)

// The whole browser path against real Postgres and restic: the bundle the
// worker writes beside the repository unlocks a session, which lists the
// snapshot and restores it.
func TestRecoveryUnlockAndRestoreOverHTTP(t *testing.T) {
	requireBinary(t, "pg_dump")
	requireBinary(t, "pg_restore")
	r := resticForTest(t)
	pool, liveURL := testdb.NewWithDSN(t)
	ctx := context.Background()
	name, _ := recovery.DatabaseName(liveURL)
	if _, err := pool.Exec(ctx, `INSERT INTO admin_accounts (id, email, full_name, password_hash) VALUES (gen_random_uuid(), 'first@hospital.test', 'First', 'x')`); err != nil {
		t.Fatal(err)
	}

	secrets := backup.NewRecoverySecrets([]byte("0123456789abcdef0123456789abcdef"), "pepper", []byte("cred-key-cred-key-cred-key-cred-"), []byte("totp-key-totp-key-totp-key-totp-"))
	key, _ := backup.NewRecoveryKey()
	bundle, err := backup.SealRecoveryBundle(key, secrets, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if rep, err := backup.RunBackup(ctx, backup.Options{DatabaseURL: liveURL, BackupDir: dir, Restic: r, RecoveryBundle: bundle}, time.Now().UTC()); err != nil || rep.Outcome != backup.OutcomeSuccess {
		t.Fatalf("RunBackup: %+v %v", rep, err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO admin_accounts (id, email, full_name, password_hash) VALUES (gen_random_uuid(), 'second@hospital.test', 'Second', 'x')`); err != nil {
		t.Fatal(err)
	}
	pool.Close()
	t.Cleanup(func() {
		ops := &recovery.PGOps{LiveURL: liveURL}
		st, ok, _ := recovery.ReadState(filepath.Join(dir, recovery.StateFile))
		if ok && st.OutgoingDB != "" {
			_ = ops.Drop(context.Background(), st.OutgoingDB)
		}
	})

	discard := slog.New(slog.NewTextHandler(io.Discard, nil))
	ops := &recovery.PGOps{LiveURL: liveURL, BackupDir: dir, Restic: r, Migrate: db.Migrate}
	engine := &recovery.Engine{
		StatePath: filepath.Join(dir, recovery.StateFile), LiveDB: name, Ops: ops,
		Now: time.Now, Logger: discard, Base: ctx,
	}
	api := &recovery.Handler{
		Engine: engine, Status: ops.LiveState, Worker: func() string { return "ready" },
		Sources: func(ctx context.Context) []recovery.Source {
			return recovery.DiscoverSources(ctx, dir, nil, ops.Destinations)
		},
		Snapshots: func(ctx context.Context, s recovery.Source) ([]backup.Snapshot, error) {
			return r.Snapshots(ctx, s.Repo())
		},
		Secrets: secrets,
		Limiter: &recovery.Limiter{PerIP: 5, Total: 20, Now: time.Now},
		Now:     time.Now, Logger: discard,
	}
	srv := httptest.NewServer(api.Routes())
	defer srv.Close()

	call := func(method, path, body string, c *http.Cookie) (*http.Response, map[string]any) {
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		if c != nil {
			req.AddCookie(c) // Secure cookies are not sent over http by a jar; set it by hand.
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close() //nolint:errcheck
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp, out
	}

	resp, status := call(http.MethodGet, "/recovery/api/status", "", nil)
	if resp.StatusCode != http.StatusOK || status["database"] != "working" {
		t.Fatalf("status = %d %v", resp.StatusCode, status)
	}
	resp, _ = call(http.MethodPost, "/recovery/api/unlock", `{"source":"local","key":"`+key.String()+`"}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unlock = %d", resp.StatusCode)
	}
	var cookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "hdms_recovery" {
			cookie = c
		}
	}
	_, snaps := call(http.MethodGet, "/recovery/api/snapshots", "", cookie)
	list, _ := snaps["snapshots"].([]any)
	if len(list) != 1 {
		t.Fatalf("snapshots = %v", snaps)
	}
	id := list[0].(map[string]any)["id"].(string)

	resp, _ = call(http.MethodPost, "/recovery/api/restore", `{"snapshotId":"`+id+`","confirmation":"RESTORE"}`, cookie)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("restore = %d", resp.StatusCode)
	}
	deadline := time.Now().Add(2 * time.Minute)
	for {
		_, state := call(http.MethodGet, "/recovery/api/restore", "", cookie)
		restore, _ := state["restore"].(map[string]any)
		if restore["phase"] == "completed" {
			if restore["canUndo"] != true {
				t.Fatalf("canUndo = %v after a restore over a working database", restore["canUndo"])
			}
			break
		}
		if restore["phase"] == "failed" || time.Now().After(deadline) {
			t.Fatalf("restore did not complete: %v", restore)
		}
		time.Sleep(200 * time.Millisecond)
	}
	live := connectTo(t, liveURL)
	if n := countOf(t, live, `SELECT count(*) FROM admin_accounts`); n != 1 {
		t.Fatalf("admins after restore = %d, want 1", n)
	}
}
