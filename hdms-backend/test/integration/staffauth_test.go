//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/staffauth"
	"github.com/hito-hospital/hdms/test/testdb"
)

func newStaffAuth(t *testing.T) (*staffauth.Service, string) {
	t.Helper()
	pool := testdb.New(t)
	var userID string
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO users (id, employee_no, full_name, registered_by)
		VALUES (gen_random_uuid(), 'E-SA-1', 'Test Staff', 'admin:test') RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return staffauth.New(pool, time.Hour), userID
}

func TestPasswordLoginIssuesAStaffSession(t *testing.T) {
	svc, userID := newStaffAuth(t)
	ctx := context.Background()

	account, err := svc.EnsureAccount(ctx, userID, "admin:test", true)
	if err != nil {
		t.Fatalf("EnsureAccount: %v", err)
	}
	if err := svc.SetPassword(ctx, account.ID, "correct-horse-battery", true); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	if err := svc.VerifyPassword(ctx, account.ID, "correct-horse-battery"); err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}

	sessionToken, csrfToken, err := svc.StartSession(ctx, account.ID)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	validated, err := svc.ValidateSession(ctx, sessionToken)
	if err != nil {
		t.Fatalf("ValidateSession: %v", err)
	}
	if validated.Account.ID != account.ID || validated.CSRFToken != csrfToken {
		t.Fatalf("validated session = %+v, want account %s and csrf %s", validated, account.ID, csrfToken)
	}
	if !validated.Account.MustChangePassword {
		t.Fatal("a password set with mustChange=true must surface MustChangePassword")
	}

	if err := svc.RevokeSession(ctx, sessionToken); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}
	if _, err := svc.ValidateSession(ctx, sessionToken); !errors.Is(err, staffauth.ErrSessionInvalid) {
		t.Fatalf("ValidateSession after revoke = %v, want ErrSessionInvalid", err)
	}
}

func TestRepeatedBadPasswordsLockTheStaffAccount(t *testing.T) {
	svc, userID := newStaffAuth(t)
	ctx := context.Background()

	account, err := svc.EnsureAccount(ctx, userID, "admin:test", true)
	if err != nil {
		t.Fatalf("EnsureAccount: %v", err)
	}
	if err := svc.SetPassword(ctx, account.ID, "correct-horse-battery", false); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}

	for i := 0; i < staffauth.MaxFailedAttempts; i++ {
		if err := svc.VerifyPassword(ctx, account.ID, "wrong"); !errors.Is(err, staffauth.ErrInvalidCredentials) {
			t.Fatalf("attempt %d = %v, want ErrInvalidCredentials", i+1, err)
		}
	}
	if err := svc.VerifyPassword(ctx, account.ID, "correct-horse-battery"); !errors.Is(err, staffauth.ErrAccountLocked) {
		t.Fatalf("after %d failures the correct password = %v, want ErrAccountLocked", staffauth.MaxFailedAttempts, err)
	}
}

func TestAMicrosoftOnlyAccountHasNoPasswordToVerify(t *testing.T) {
	svc, userID := newStaffAuth(t)
	ctx := context.Background()

	account, err := svc.EnsureAccount(ctx, userID, "self:microsoft", false)
	if err != nil {
		t.Fatalf("EnsureAccount: %v", err)
	}
	if account.HasPassword {
		t.Fatal("a freshly provisioned Microsoft account must have no password")
	}
	if err := svc.VerifyPassword(ctx, account.ID, "anything"); !errors.Is(err, staffauth.ErrNoPasswordSet) {
		t.Fatalf("VerifyPassword on a passwordless account = %v, want ErrNoPasswordSet", err)
	}
}
