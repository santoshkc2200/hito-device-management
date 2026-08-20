//go:build integration

package integration

import (
	"context"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/auth"
	"github.com/hito-hospital/hdms/test/testdb"
	"github.com/pquerna/otp/totp"
)

func newAuthService(t *testing.T) *auth.Service {
	t.Helper()
	pool := testdb.New(t)
	encKey := make([]byte, 32)
	if _, err := rand.Read(encKey); err != nil {
		t.Fatalf("generate totp enc key: %v", err)
	}
	return auth.New(pool, "dev-only-pepper", encKey, time.Hour)
}

func TestSessionCreateValidateSlideRevoke(t *testing.T) {
	svc := newAuthService(t)
	ctx := context.Background()

	sessionToken, csrfToken, admin, err := loginWithFreshTOTP(t, svc)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if csrfToken == "" || admin.Email == "" {
		t.Fatal("expected a csrf token and admin identity from Login")
	}

	validated, err := svc.ValidateSession(ctx, sessionToken)
	if err != nil {
		t.Fatalf("ValidateSession: %v", err)
	}
	if validated.Admin.ID != admin.ID {
		t.Fatalf("validated admin ID = %q, want %q", validated.Admin.ID, admin.ID)
	}
	if validated.CSRFToken != csrfToken {
		t.Fatalf("validated CSRF token = %q, want %q", validated.CSRFToken, csrfToken)
	}

	if err := svc.RevokeSession(ctx, sessionToken); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}

	if _, err := svc.ValidateSession(ctx, sessionToken); err == nil {
		t.Fatal("expected ValidateSession to fail after revoke")
	}
}

func TestMiddlewareRejectsCSRFMismatchOnMutatingRequest(t *testing.T) {
	svc := newAuthService(t)
	sessionToken, csrfToken, _, err := loginWithFreshTOTP(t, svc)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	handler := svc.Middleware(next)

	// Mismatched CSRF header on a mutating request must be rejected.
	req := httptest.NewRequest(http.MethodPost, "/v1/devices", nil)
	req.AddCookie(&http.Cookie{Name: "hdms_session", Value: sessionToken})
	req.Header.Set("X-CSRF-Token", "wrong-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("mismatched CSRF: status = %d, want 403", rec.Code)
	}
	if called {
		t.Fatal("handler must not run when CSRF check fails")
	}

	// A matching CSRF header must pass.
	called = false
	req = httptest.NewRequest(http.MethodPost, "/v1/devices", nil)
	req.AddCookie(&http.Cookie{Name: "hdms_session", Value: sessionToken})
	req.Header.Set("X-CSRF-Token", csrfToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("matching CSRF: status = %d, want 200", rec.Code)
	}
	if !called {
		t.Fatal("handler must run when CSRF check passes")
	}
}

func TestAdminIdentityCarriesRoleFromSession(t *testing.T) {
	svc := newAuthService(t)
	ctx := context.Background()

	roles := []string{"admin", "technician", "viewer"}
	for _, r := range roles {
		t.Run("role_"+r, func(t *testing.T) {
			email := r + "@example.org"
			password := "password123456"
			_, secret, _, err := svc.CreateAdminAccount(ctx, email, "Test "+r, password, r)
			if err != nil {
				t.Fatalf("CreateAdminAccount: %v", err)
			}
			code, err := currentTOTPCode(secret)
			if err != nil {
				t.Fatalf("currentTOTPCode: %v", err)
			}
			sessionToken, _, admin, err := svc.Login(ctx, email, password, code)
			if err != nil {
				t.Fatalf("Login: %v", err)
			}
			if admin.Role != r {
				t.Fatalf("login admin.Role = %q, want %q", admin.Role, r)
			}
			validated, err := svc.ValidateSession(ctx, sessionToken)
			if err != nil {
				t.Fatalf("ValidateSession: %v", err)
			}
			if validated.Admin.Role != r {
				t.Fatalf("validated admin.Role = %q, want %q", validated.Admin.Role, r)
			}
		})
	}
}

// loginWithFreshTOTP bootstraps an admin account and logs in, computing a
// valid TOTP code from the secret returned at bootstrap — Login has no
// other way to succeed, since the secret is otherwise never retrievable.
func loginWithFreshTOTP(t *testing.T, svc *auth.Service) (sessionToken, csrfToken string, admin auth.AdminIdentity, err error) {
	t.Helper()
	ctx := context.Background()
	email := "admin@example.org"
	password := "correct horse battery staple"

	_, secret, _, err := svc.CreateAdminAccount(ctx, email, "Test Admin", password, "admin")
	if err != nil {
		t.Fatalf("CreateAdminAccount: %v", err)
	}

	code, err := currentTOTPCode(secret)
	if err != nil {
		t.Fatalf("currentTOTPCode: %v", err)
	}

	return svc.Login(ctx, email, password, code)
}

func currentTOTPCode(secret string) (string, error) {
	return totp.GenerateCode(secret, time.Now())
}
