//go:build integration

package integration

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// The two refusals that keep the realms apart. If either regresses, a staff
// phone can reach the admin console or vice versa.
func TestAnAdminCookieIsRejectedOnStaffRoutes(t *testing.T) {
	env := newHTTPTestEnv(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/staff/me", nil)
	req.AddCookie(&http.Cookie{Name: "hdms_session", Value: env.AdminSessionToken})

	rec := httptest.NewRecorder()
	env.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET /v1/staff/me with an admin cookie = %d, want 401", rec.Code)
	}
}

func TestAStaffCookieIsRejectedOnAdminRoutes(t *testing.T) {
	env := newHTTPTestEnv(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/users", nil)
	req.AddCookie(&http.Cookie{Name: "hdms_staff_session", Value: env.StaffSessionToken})

	rec := httptest.NewRecorder()
	env.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET /v1/users with a staff cookie = %d, want 401", rec.Code)
	}
}
