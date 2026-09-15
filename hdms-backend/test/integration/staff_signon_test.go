//go:build integration

package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/hito-hospital/hdms/internal/platform/staffauth"
)

// The ladder: an existing link wins, then the employee-number claim, then the
// email, and only then is a user provisioned.
func TestMicrosoftSignInLinksToAnExistingUserByEmployeeNumber(t *testing.T) {
	env := newHTTPTestEnv(t)
	ctx := context.Background()

	existing := env.SeedUser(t, "E-LINK-1", "Existing Person", "existing@hospital.example")

	user, account, err := env.Server.ResolveOrProvisionForTest(ctx, staffauth.Claims{
		Subject: "oid-link-1", TenantID: env.TenantID,
		Email: "someone.else@hospital.example", DisplayName: "Existing Person",
		EmployeeNo: "E-LINK-1",
	})
	if err != nil {
		t.Fatalf("resolveOrProvision: %v", err)
	}
	if user.ID != existing.ID {
		t.Fatalf("linked to user %s, want the existing %s", user.ID, existing.ID)
	}
	if !account.ProfileComplete {
		t.Fatal("linking to an existing user must leave the profile complete")
	}
}

func TestMicrosoftSignInLinksToAnExistingUserByEmail(t *testing.T) {
	env := newHTTPTestEnv(t)
	ctx := context.Background()

	existing := env.SeedUser(t, "E-LINK-2", "Email Person", "email.person@hospital.example")

	user, _, err := env.Server.ResolveOrProvisionForTest(ctx, staffauth.Claims{
		Subject: "oid-link-2", TenantID: env.TenantID,
		Email: "EMAIL.PERSON@hospital.example", DisplayName: "Email Person",
	})
	if err != nil {
		t.Fatalf("resolveOrProvision: %v", err)
	}
	if user.ID != existing.ID {
		t.Fatalf("linked to user %s, want the existing %s — the email match is case-insensitive", user.ID, existing.ID)
	}
}

func TestMicrosoftCallbackProvisionsAUserExactlyOnce(t *testing.T) {
	env := newHTTPTestEnv(t)
	ctx := context.Background()
	claims := staffauth.Claims{
		Subject: "oid-race", TenantID: env.TenantID,
		Email: "race@hospital.example", DisplayName: "Race Person",
	}

	var wg sync.WaitGroup
	ids := make([]string, 4)
	errs := make([]error, 4)
	for i := range ids {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			user, _, err := env.Server.ResolveOrProvisionForTest(ctx, claims)
			ids[i], errs[i] = user.ID, err
		}(i)
	}
	wg.Wait()

	seen := map[string]bool{}
	for i := range ids {
		if errs[i] != nil {
			continue // one loser in a race is acceptable; two users are not
		}
		seen[ids[i]] = true
	}
	if len(seen) != 1 {
		t.Fatalf("concurrent callbacks produced %d distinct users, want exactly 1", len(seen))
	}
}

func TestAProvisionedUserWithNoEmployeeNumberClaimHasNoCredentialUntilCompletion(t *testing.T) {
	env := newHTTPTestEnv(t)
	ctx := context.Background()

	user, account, err := env.Server.ResolveOrProvisionForTest(ctx, staffauth.Claims{
		Subject: "oid-incomplete", TenantID: env.TenantID,
		Email: "incomplete@hospital.example", DisplayName: "Incomplete Person",
	})
	if err != nil {
		t.Fatalf("resolveOrProvision: %v", err)
	}
	if account.ProfileComplete {
		t.Fatal("a provisioning with no employee-number claim must leave the profile incomplete")
	}
	if n := env.CountActiveCredentials(t, user.ID); n != 0 {
		t.Fatalf("active credentials = %d, want 0 until the profile is complete", n)
	}
}

func TestASuspendedUserCannotSignIn(t *testing.T) {
	env := newHTTPTestEnv(t)
	ctx := context.Background()

	user := env.SeedUser(t, "E-SUSP-1", "Suspended Person", "suspended@hospital.example")
	account := env.SeedStaffAccountWithPassword(t, user.ID, "a-long-enough-password")
	_ = account
	if _, err := env.Identity.SuspendUser(ctx, user.ID, "left the ward", "admin:test"); err != nil {
		t.Fatalf("SuspendUser: %v", err)
	}

	body := strings.NewReader(`{"employeeNo":"E-SUSP-1","password":"a-long-enough-password"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/staff/auth/password", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	env.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("login as a suspended user = %d, want 401", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "suspend") {
		t.Fatalf("the refusal explained that the account is suspended: %s", rec.Body.String())
	}
}
