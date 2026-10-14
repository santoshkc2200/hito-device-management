//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/hito-hospital/hdms/internal/platform/staffauth"
)

func TestEightCharacterStaffPasswords(t *testing.T) {
	env := newHTTPTestEnv(t)
	ctx := context.Background()
	user := env.SeedUser(t, "E-EIGHT-1", "Eight Person", "eight@hospital.example")
	temporary, err := env.Server.ResetStaffPasswordForTest(ctx, user.ID, "admin:test")
	if err != nil {
		t.Fatal(err)
	}
	if len(temporary) != 8 {
		t.Fatalf("temporary password length = %d, want 8", len(temporary))
	}
	account, err := env.StaffAuth.AccountForUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.StaffAuth.ChangeOwnPassword(ctx, account.ID, temporary, "seven12"); !errors.Is(err, staffauth.ErrWeakPassword) {
		t.Fatalf("seven-character password: %v", err)
	}
	if err := env.StaffAuth.ChangeOwnPassword(ctx, account.ID, temporary, "eight123"); err != nil {
		t.Fatal(err)
	}
	if err := env.StaffAuth.VerifyPassword(ctx, account.ID, "eight123"); err != nil {
		t.Fatal(err)
	}
	account, err = env.StaffAuth.AccountForUser(ctx, user.ID)
	if err != nil || account.MustChangePassword {
		t.Fatalf("password change did not clear first-login requirement: %v", err)
	}
}

func TestAPasswordResetEndsEveryExistingStaffSession(t *testing.T) {
	env := newHTTPTestEnv(t)
	ctx := context.Background()

	user := env.SeedUser(t, "E-RESET-1", "Reset Person", "reset@hospital.example")
	account := env.SeedStaffAccountWithPassword(t, user.ID, "old-password-here")
	token, _, err := env.StaffAuth.StartSession(ctx, account.ID)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	if _, err := env.Server.ResetStaffPasswordForTest(ctx, user.ID, "admin:test"); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if _, err := env.StaffAuth.ValidateSession(ctx, token); err == nil {
		t.Fatal("a session survived a password reset; every session must end")
	}
}
