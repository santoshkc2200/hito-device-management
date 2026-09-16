//go:build integration

package integration

import (
	"context"
	"testing"
)

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
