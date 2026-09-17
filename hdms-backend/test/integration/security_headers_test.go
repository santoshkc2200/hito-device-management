//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

// TestSecurityHeadersAcrossRoutes verifies that standard hardening headers
// (HSTS, CSP, X-Content-Type-Options, Referrer-Policy, X-Frame-Options)
// are consistently applied across app routes, static routes, and error routes.
func TestSecurityHeadersAcrossRoutes(t *testing.T) {
	h := newTestHarness(t)

	routes := []struct {
		name       string
		method     string
		path       string
		withAuth   bool
		wantStatus int
	}{
		{
			name:       "app route (authenticated /v1/auth/me)",
			method:     http.MethodGet,
			path:       "/v1/auth/me",
			withAuth:   true,
			wantStatus: http.StatusOK,
		},
		{
			name:       "static / unauthenticated route (/v1/readyz)",
			method:     http.MethodGet,
			path:       "/v1/readyz",
			withAuth:   false,
			wantStatus: http.StatusOK,
		},
		{
			name:       "error route 404 (/v1/devices/{nonexistent_id})",
			method:     http.MethodGet,
			path:       "/v1/devices/00000000-0000-0000-0000-000000000000",
			withAuth:   true,
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "error route 401 unauthenticated app route (/v1/devices)",
			method:     http.MethodGet,
			path:       "/v1/devices",
			withAuth:   false,
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tc := range routes {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, h.server.URL+tc.path, nil)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			if tc.withAuth {
				req.AddCookie(&http.Cookie{Name: "hdms_session", Value: h.adminSessionToken})
			}

			// Use a raw client to avoid automatic cookie management interfering with unauthenticated cases
			client := &http.Client{}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("client.Do: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tc.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}

			// Assert security headers
			hsts := resp.Header.Get("Strict-Transport-Security")
			if hsts != "max-age=31536000; includeSubDomains; preload" {
				t.Errorf("Strict-Transport-Security = %q, want max-age=31536000; includeSubDomains; preload", hsts)
			}

			nosniff := resp.Header.Get("X-Content-Type-Options")
			if nosniff != "nosniff" {
				t.Errorf("X-Content-Type-Options = %q, want nosniff", nosniff)
			}

			referrer := resp.Header.Get("Referrer-Policy")
			if referrer != "strict-origin-when-cross-origin" {
				t.Errorf("Referrer-Policy = %q, want strict-origin-when-cross-origin", referrer)
			}

			frameOptions := resp.Header.Get("X-Frame-Options")
			if frameOptions != "DENY" {
				t.Errorf("X-Frame-Options = %q, want DENY", frameOptions)
			}

			csp := resp.Header.Get("Content-Security-Policy")
			if csp == "" {
				t.Error("Content-Security-Policy header is missing")
			}
			if strings.Contains(csp, "'unsafe-inline'") {
				t.Errorf("Content-Security-Policy contains 'unsafe-inline': %s", csp)
			}
			if !strings.Contains(csp, "frame-ancestors 'none'") {
				t.Errorf("Content-Security-Policy missing frame-ancestors 'none': %s", csp)
			}
			if !strings.Contains(csp, "media-src 'self' blob:") {
				t.Errorf("Content-Security-Policy missing media-src camera/blob support: %s", csp)
			}
			if !strings.Contains(csp, "worker-src 'self'") {
				t.Errorf("Content-Security-Policy missing worker-src service worker support: %s", csp)
			}
		})
	}
}

// TestSessionIdentifierChangesOnLogin asserts that session identifiers are regenerated
// on login (session fixation prevention) and on privilege changes, with proper cookie flags.
func TestSessionIdentifierChangesOnLogin(t *testing.T) {
	h := newTestHarness(t)

	email := "sec-admin@example.org"
	password := "correct horse battery staple"
	adminID, secret, _, err := h.auth.CreateAdminAccount(t.Context(), email, "Security Admin", password, "admin")
	if err != nil {
		t.Fatalf("CreateAdminAccount: %v", err)
	}

	code1, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("GenerateCode 1: %v", err)
	}

	// 1. First login
	loginReqBody, _ := json.Marshal(map[string]string{
		"email": email, "password": password, "totpCode": code1,
	})
	req1, _ := http.NewRequest(http.MethodPost, h.server.URL+"/v1/auth/login", bytes.NewReader(loginReqBody))
	req1.Header.Set("Content-Type", "application/json")

	rawClient := &http.Client{}
	resp1, err := rawClient.Do(req1)
	if err != nil {
		t.Fatalf("login 1 failed: %v", err)
	}
	defer resp1.Body.Close()

	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("login 1 status = %d, want 200", resp1.StatusCode)
	}

	var session1, csrf1 string
	for _, c := range resp1.Cookies() {
		if c.Name == "hdms_session" {
			session1 = c.Value
			if !c.HttpOnly {
				t.Errorf("hdms_session HttpOnly = false, want true")
			}
			if !c.Secure {
				t.Errorf("hdms_session Secure = false, want true")
			}
			if c.SameSite != http.SameSiteLaxMode {
				t.Errorf("hdms_session SameSite = %v, want SameSiteLaxMode", c.SameSite)
			}
		}
		if c.Name == "hdms_csrf" {
			csrf1 = c.Value
			if c.HttpOnly {
				t.Errorf("hdms_csrf HttpOnly = true, want false (must be readable by JS)")
			}
			if !c.Secure {
				t.Errorf("hdms_csrf Secure = false, want true")
			}
			if c.SameSite != http.SameSiteLaxMode {
				t.Errorf("hdms_csrf SameSite = %v, want SameSiteLaxMode", c.SameSite)
			}
		}
	}
	if session1 == "" || csrf1 == "" {
		t.Fatalf("login 1 did not return session or csrf cookies: session=%q, csrf=%q", session1, csrf1)
	}

	// Verify session1 works
	meReq1, _ := http.NewRequest(http.MethodGet, h.server.URL+"/v1/auth/me", nil)
	meReq1.AddCookie(&http.Cookie{Name: "hdms_session", Value: session1})
	meResp1, err := rawClient.Do(meReq1)
	if err != nil {
		t.Fatalf("auth/me 1: %v", err)
	}
	defer meResp1.Body.Close()
	if meResp1.StatusCode != http.StatusOK {
		t.Fatalf("auth/me with session1 status = %d, want 200", meResp1.StatusCode)
	}

	// 2. Second login for the same admin, sending the previous session cookie
	code2, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("GenerateCode 2: %v", err)
	}
	loginReqBody2, _ := json.Marshal(map[string]string{
		"email": email, "password": password, "totpCode": code2,
	})
	req2, _ := http.NewRequest(http.MethodPost, h.server.URL+"/v1/auth/login", bytes.NewReader(loginReqBody2))
	req2.Header.Set("Content-Type", "application/json")
	req2.AddCookie(&http.Cookie{Name: "hdms_session", Value: session1})

	resp2, err := rawClient.Do(req2)
	if err != nil {
		t.Fatalf("login 2 failed: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("login 2 status = %d, want 200", resp2.StatusCode)
	}

	var session2 string
	for _, c := range resp2.Cookies() {
		if c.Name == "hdms_session" {
			session2 = c.Value
		}
	}
	if session2 == "" {
		t.Fatal("login 2 did not return hdms_session cookie")
	}

	// Assert session identifier regenerated (session fixation prevention)
	if session2 == session1 {
		t.Fatalf("session identifier was NOT regenerated on login: session1 = session2 = %q", session1)
	}

	// Verify session2 is valid
	meReq2, _ := http.NewRequest(http.MethodGet, h.server.URL+"/v1/auth/me", nil)
	meReq2.AddCookie(&http.Cookie{Name: "hdms_session", Value: session2})
	meResp2, err := rawClient.Do(meReq2)
	if err != nil {
		t.Fatalf("auth/me 2: %v", err)
	}
	defer meResp2.Body.Close()
	if meResp2.StatusCode != http.StatusOK {
		t.Fatalf("auth/me with session2 status = %d, want 200", meResp2.StatusCode)
	}

	// Assert previous session1 was invalidated
	meReqStale, _ := http.NewRequest(http.MethodGet, h.server.URL+"/v1/auth/me", nil)
	meReqStale.AddCookie(&http.Cookie{Name: "hdms_session", Value: session1})
	meRespStale, err := rawClient.Do(meReqStale)
	if err != nil {
		t.Fatalf("auth/me stale: %v", err)
	}
	defer meRespStale.Body.Close()
	if meRespStale.StatusCode != http.StatusUnauthorized {
		t.Fatalf("stale session1 status = %d, want 401 Unauthorized", meRespStale.StatusCode)
	}

	// 3. Privilege change: update admin's own role and assert session identifier regenerates
	var csrf2 string
	for _, c := range resp2.Cookies() {
		if c.Name == "hdms_csrf" {
			csrf2 = c.Value
		}
	}
	updateRoleBody, _ := json.Marshal(map[string]string{
		"role": "technician",
	})
	patchReq, _ := http.NewRequest(http.MethodPatch, h.server.URL+"/v1/admins/"+adminID, bytes.NewReader(updateRoleBody))
	patchReq.Header.Set("Content-Type", "application/json")
	patchReq.Header.Set("X-CSRF-Token", csrf2)
	patchReq.AddCookie(&http.Cookie{Name: "hdms_session", Value: session2})

	patchResp, err := rawClient.Do(patchReq)
	if err != nil {
		t.Fatalf("patch admin failed: %v", err)
	}
	defer patchResp.Body.Close()

	if patchResp.StatusCode != http.StatusOK {
		t.Fatalf("patch admin status = %d, want 200", patchResp.StatusCode)
	}

	var session3 string
	for _, c := range patchResp.Cookies() {
		if c.Name == "hdms_session" {
			session3 = c.Value
		}
	}
	if session3 == "" {
		t.Fatal("privilege change did not set a new hdms_session cookie")
	}
	if session3 == session2 {
		t.Fatalf("session identifier was NOT regenerated on privilege change: session2 = session3 = %q", session2)
	}

	// Verify session3 works
	meReq3, _ := http.NewRequest(http.MethodGet, h.server.URL+"/v1/auth/me", nil)
	meReq3.AddCookie(&http.Cookie{Name: "hdms_session", Value: session3})
	meResp3, err := rawClient.Do(meReq3)
	if err != nil {
		t.Fatalf("auth/me 3: %v", err)
	}
	defer meResp3.Body.Close()
	if meResp3.StatusCode != http.StatusOK {
		t.Fatalf("auth/me with session3 status = %d, want 200", meResp3.StatusCode)
	}

	// Verify previous session2 is now revoked
	meReqStale2, _ := http.NewRequest(http.MethodGet, h.server.URL+"/v1/auth/me", nil)
	meReqStale2.AddCookie(&http.Cookie{Name: "hdms_session", Value: session2})
	meRespStale2, err := rawClient.Do(meReqStale2)
	if err != nil {
		t.Fatalf("auth/me stale2: %v", err)
	}
	defer meRespStale2.Body.Close()
	if meRespStale2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("stale session2 after privilege change status = %d, want 401 Unauthorized", meRespStale2.StatusCode)
	}
}

// TestStateChangingRequestWithoutCsrfIsRejected asserts that all state-changing routes
// reject requests lacking or having mismatched CSRF tokens with 403 Forbidden.
func TestStateChangingRequestWithoutCsrfIsRejected(t *testing.T) {
	h := newTestHarness(t)

	routes := []struct {
		name   string
		method string
		path   string
		body   map[string]any
	}{
		{
			name:   "POST /v1/devices",
			method: http.MethodPost,
			path:   "/v1/devices",
			body: map[string]any{
				"model":        "Test Model",
				"serialNumber": "SN-CSRF-1",
				"status":       "available",
			},
		},
		{
			name:   "POST /v1/users",
			method: http.MethodPost,
			path:   "/v1/users",
			body: map[string]any{
				"employeeNo": "E-CSRF-1",
				"fullName":   "CSRF Test User",
				"email":      "csrf1@example.org",
			},
		},
		{
			name:   "PATCH /v1/admins/{id}",
			method: http.MethodPatch,
			path:   "/v1/admins/00000000-0000-0000-0000-000000000001",
			body: map[string]any{
				"fullName": "New Name",
			},
		},
		{
			name:   "POST /v1/categories",
			method: http.MethodPost,
			path:   "/v1/categories",
			body: map[string]any{
				"name": "CSRF Category",
			},
		},
		{
			name:   "POST /v1/auth/totp/reenrol",
			method: http.MethodPost,
			path:   "/v1/auth/totp/reenrol",
			body:   nil,
		},
		{
			name:   "POST /v1/auth/logout",
			method: http.MethodPost,
			path:   "/v1/auth/logout",
			body:   nil,
		},
	}

	rawClient := &http.Client{}

	for _, tc := range routes {
		t.Run(tc.name, func(t *testing.T) {
			var bodyBytes []byte
			if tc.body != nil {
				bodyBytes, _ = json.Marshal(tc.body)
			}

			// Case 1: Missing CSRF token -> 403 Forbidden
			reqNoCsrf, _ := http.NewRequest(tc.method, h.server.URL+tc.path, bytes.NewReader(bodyBytes))
			reqNoCsrf.Header.Set("Content-Type", "application/json")
			reqNoCsrf.AddCookie(&http.Cookie{Name: "hdms_session", Value: h.adminSessionToken})

			respNoCsrf, err := rawClient.Do(reqNoCsrf)
			if err != nil {
				t.Fatalf("request without CSRF: %v", err)
			}
			defer respNoCsrf.Body.Close()

			if respNoCsrf.StatusCode != http.StatusForbidden {
				t.Errorf("missing CSRF status = %d, want 403 Forbidden", respNoCsrf.StatusCode)
			}

			// Case 2: Mismatched CSRF token -> 403 Forbidden
			reqBadCsrf, _ := http.NewRequest(tc.method, h.server.URL+tc.path, bytes.NewReader(bodyBytes))
			reqBadCsrf.Header.Set("Content-Type", "application/json")
			reqBadCsrf.Header.Set("X-CSRF-Token", "invalid-csrf-token-value")
			reqBadCsrf.AddCookie(&http.Cookie{Name: "hdms_session", Value: h.adminSessionToken})

			respBadCsrf, err := rawClient.Do(reqBadCsrf)
			if err != nil {
				t.Fatalf("request with bad CSRF: %v", err)
			}
			defer respBadCsrf.Body.Close()

			if respBadCsrf.StatusCode != http.StatusForbidden {
				t.Errorf("mismatched CSRF status = %d, want 403 Forbidden", respBadCsrf.StatusCode)
			}

			// Case 3: Valid CSRF token -> NOT 403 Forbidden
			reqValidCsrf, _ := http.NewRequest(tc.method, h.server.URL+tc.path, bytes.NewReader(bodyBytes))
			reqValidCsrf.Header.Set("Content-Type", "application/json")
			reqValidCsrf.Header.Set("X-CSRF-Token", h.csrfToken)
			reqValidCsrf.AddCookie(&http.Cookie{Name: "hdms_session", Value: h.adminSessionToken})

			respValidCsrf, err := rawClient.Do(reqValidCsrf)
			if err != nil {
				t.Fatalf("request with valid CSRF: %v", err)
			}
			defer respValidCsrf.Body.Close()

			if respValidCsrf.StatusCode == http.StatusForbidden {
				t.Errorf("valid CSRF got 403 Forbidden unexpectedly on %s %s", tc.method, tc.path)
			}
		})
	}
}
