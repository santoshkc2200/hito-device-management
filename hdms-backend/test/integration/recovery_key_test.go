//go:build integration

package integration

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/hito-hospital/hdms/internal/platform/auth"
	"github.com/hito-hospital/hdms/internal/platform/backup"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
	"github.com/hito-hospital/hdms/test/testdb"
)

func TestRecoveryKeyStore(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	if _, err := backup.GetRecoveryKey(ctx, pool.Pool); !errors.Is(err, backup.ErrNoRecoveryKey) {
		t.Fatalf("empty get err = %v", err)
	}
	if _, err := backup.ConfirmRecoveryKey(ctx, pool.Pool, time.Now()); !errors.Is(err, backup.ErrNoRecoveryKey) {
		t.Fatalf("confirm with none err = %v", err)
	}

	first, err := backup.SaveRecoveryKey(ctx, pool.Pool, []byte("bundle-1"), "fp-1", "admin:a")
	if err != nil || string(first.Bundle) != "bundle-1" || first.ConfirmedAt != nil || first.CreatedBy != "admin:a" {
		t.Fatalf("save = %+v, %v", first, err)
	}
	confirmed, err := backup.ConfirmRecoveryKey(ctx, pool.Pool, time.Now())
	if err != nil || confirmed.ConfirmedAt == nil {
		t.Fatalf("confirm = %+v, %v", confirmed, err)
	}

	// Replacing clears the confirmation: the new sheet has not been printed yet.
	second, err := backup.SaveRecoveryKey(ctx, pool.Pool, []byte("bundle-2"), "fp-2", "admin:b")
	if err != nil || string(second.Bundle) != "bundle-2" || second.ConfirmedAt != nil || second.Fingerprint != "fp-2" {
		t.Fatalf("replace = %+v, %v", second, err)
	}
	var rows int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM backup_recovery_key`).Scan(&rows)
	if rows != 1 {
		t.Fatalf("rows = %d, want exactly one", rows)
	}
}

func TestRecoveryKeyStatus(t *testing.T) {
	current := backup.RecoverySecrets{BackupEncKey: "a", TokenPepper: "b", CredentialEncKey: "c", TOTPEncKey: "d"}
	now := time.Now()
	for _, tc := range []struct {
		name string
		rec  *backup.RecoveryKeyRecord
		want string
	}{
		{"none", nil, "missing"},
		{"unconfirmed", &backup.RecoveryKeyRecord{Fingerprint: current.Fingerprint()}, "unconfirmed"},
		{"ready", &backup.RecoveryKeyRecord{Fingerprint: current.Fingerprint(), ConfirmedAt: &now}, "ready"},
		{"outdated beats confirmed", &backup.RecoveryKeyRecord{Fingerprint: "old", ConfirmedAt: &now}, "outdated"},
		{"outdated beats unconfirmed", &backup.RecoveryKeyRecord{Fingerprint: "old"}, "outdated"},
	} {
		if got := backup.RecoveryKeyStatus(tc.rec, current); got != tc.want {
			t.Errorf("%s: status = %s, want %s", tc.name, got, tc.want)
		}
	}
}

func TestReauthenticateAdmin(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	svc := auth.New(pool, "dev-only-pepper", random32(t), time.Hour)
	id, secret, _, err := svc.CreateAdminAccount(ctx, "reauth@example.org", "Re Auth", "correct horse battery staple", "admin")
	if err != nil {
		t.Fatal(err)
	}
	code := func() string {
		c, err := totp.GenerateCode(secret, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		return c
	}

	if err := svc.ReauthenticateAdmin(ctx, id, "correct horse battery staple", code()); err != nil {
		t.Fatalf("good credentials: %v", err)
	}
	if err := svc.ReauthenticateAdmin(ctx, id, "correct horse battery staple", ""); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("missing code err = %v", err)
	}
	if err := svc.ReauthenticateAdmin(ctx, "00000000-0000-0000-0000-000000000000", "x", "000000"); !errors.Is(err, auth.ErrAdminNotFound) {
		t.Fatalf("unknown admin err = %v", err)
	}

	// Wrong passwords count toward the same lockout as failed logins.
	var last error
	for i := 0; i < auth.MaxFailedAttempts; i++ {
		last = svc.ReauthenticateAdmin(ctx, id, "wrong password here", code())
	}
	if !errors.Is(last, auth.ErrInvalidCredentials) {
		t.Fatalf("wrong password err = %v", last)
	}
	if err := svc.ReauthenticateAdmin(ctx, id, "correct horse battery staple", code()); !errors.Is(err, auth.ErrAccountLocked) {
		t.Fatalf("after %d failures err = %v, want ErrAccountLocked", auth.MaxFailedAttempts, err)
	}
}

func TestHTTPRecoveryKey(t *testing.T) {
	h := newTestHarness(t)
	code := func() string {
		c, err := totp.GenerateCode(h.adminTOTPSecret, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	status := func() string {
		return string(decodeBody[gen.BackupConfig](t, h.get(t, "/v1/backup/config")).RecoveryKey.Status)
	}

	if got := status(); got != "missing" {
		t.Fatalf("initial status = %s", got)
	}

	// Wrong password: 422, not 401 — a 401 would sign the admin out of the console.
	resp := h.post(t, "/v1/backup/recovery-key", gen.BackupReauth{Password: "wrong password here", TotpCode: code()})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("wrong password status = %d, want 422", resp.StatusCode)
	}

	resp = h.post(t, "/v1/backup/recovery-key", gen.BackupReauth{Password: h.adminPassword, TotpCode: code()})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", resp.StatusCode)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", cc)
	}
	issued := decodeBody[gen.BackupRecoveryKeyIssued](t, resp)
	key, err := backup.ParseRecoveryKey(issued.Key)
	if err != nil {
		t.Fatalf("issued key does not parse: %v", err)
	}

	// The stored bundle opens with the issued key and holds the running secrets.
	rec, err := backup.GetRecoveryKey(context.Background(), h.pool.Pool)
	if err != nil {
		t.Fatal(err)
	}
	got, err := backup.OpenRecoveryBundle(key, rec.Bundle)
	if err != nil || got != h.recoverySecrets {
		t.Fatalf("bundle opens to %+v, %v", got, err)
	}
	// Nothing in the database contains the plaintext key.
	var leaks int
	_ = h.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_events WHERE payload::text LIKE '%' || $1 || '%'`, issued.Key[:9]).Scan(&leaks)
	if leaks != 0 {
		t.Fatal("audit payload contains the recovery key")
	}

	if got := status(); got != "unconfirmed" {
		t.Fatalf("after create status = %s", got)
	}
	resp = h.post(t, "/v1/backup/recovery-key/confirm", nil)
	if resp.StatusCode != http.StatusOK || string(decodeBody[gen.BackupRecoveryKeyState](t, resp).Status) != "ready" {
		t.Fatalf("confirm status = %d", resp.StatusCode)
	}

	// Replacing issues a different key and returns to unconfirmed.
	resp = h.post(t, "/v1/backup/recovery-key", gen.BackupReauth{Password: h.adminPassword, TotpCode: code()})
	replaced := decodeBody[gen.BackupRecoveryKeyIssued](t, resp)
	if replaced.Key == issued.Key || status() != "unconfirmed" {
		t.Fatalf("replace: same key or status %s", status())
	}

	// A rotated secret makes the stored bundle outdated.
	if _, err := h.pool.Exec(context.Background(), `UPDATE backup_recovery_key SET secrets_fingerprint = 'rotated'`); err != nil {
		t.Fatal(err)
	}
	if got := status(); got != "outdated" {
		t.Fatalf("rotated status = %s, want outdated", got)
	}

	var created, replacedEvents, confirmed int
	_ = h.pool.QueryRow(context.Background(), `SELECT
		count(*) FILTER (WHERE action = 'backup.recovery_key.created'),
		count(*) FILTER (WHERE action = 'backup.recovery_key.replaced'),
		count(*) FILTER (WHERE action = 'backup.recovery_key.confirmed')
		FROM audit_events`).Scan(&created, &replacedEvents, &confirmed)
	if created != 1 || replacedEvents != 1 || confirmed != 1 {
		t.Fatalf("audit created/replaced/confirmed = %d/%d/%d, want 1/1/1", created, replacedEvents, confirmed)
	}
}

func TestHTTPRecoveryKeyConfirmWithoutKey(t *testing.T) {
	h := newTestHarness(t)
	if resp := h.post(t, "/v1/backup/recovery-key/confirm", nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("confirm with no key status = %d, want 404", resp.StatusCode)
	}
}
