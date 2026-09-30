//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/hito-hospital/hdms/internal/platform/auth"
	"github.com/hito-hospital/hdms/internal/platform/backup"
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
