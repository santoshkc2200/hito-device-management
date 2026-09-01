//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/audit/auditapi"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
	"github.com/pquerna/otp/totp"
)

type adminClient struct {
	id           string
	email        string
	role         string
	sessionToken string
	csrfToken    string
	client       *http.Client
}

func (ac *adminClient) do(t *testing.T, h *testHarness, method, path string, body any) (int, []byte) {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, h.server.URL+path, bodyReader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if ac.sessionToken != "" {
		req.AddCookie(&http.Cookie{Name: "hdms_session", Value: ac.sessionToken})
	}
	if ac.csrfToken != "" {
		req.Header.Set("X-CSRF-Token", ac.csrfToken)
	}
	resp, err := ac.client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	return resp.StatusCode, respBody
}

func createAdminAndLogin(t *testing.T, h *testHarness, email, name, role, password string) (adminClient, string, []string) {
	t.Helper()
	ctx := context.Background()
	admin, secret, _, recoveryCodes, err := h.auth.CreateAdminFull(ctx, "system", email, name, password, role)
	if err != nil {
		t.Fatalf("CreateAdminFull: %v", err)
	}

	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("totp.GenerateCode: %v", err)
	}

	sessionToken, csrfToken, _, err := h.auth.Login(ctx, email, password, code)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	return adminClient{
		id:           admin.ID,
		email:        email,
		role:         role,
		sessionToken: sessionToken,
		csrfToken:    csrfToken,
		client:       &http.Client{},
	}, secret, recoveryCodes
}

func TestAdminCRUD(t *testing.T) {
	h := newTestHarness(t)
	admin, _, _ := createAdminAndLogin(t, h, "superadmin@example.org", "Super Admin", "admin", "correct horse battery staple")

	// 1. List admins
	status, body := admin.do(t, h, http.MethodGet, "/v1/admins", nil)
	if status != http.StatusOK {
		t.Fatalf("ListAdmins status = %d, want 200: %s", status, string(body))
	}
	var list gen.AdminList
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("unmarshal AdminList: %v", err)
	}
	if len(list.Items) < 1 {
		t.Fatalf("expected at least 1 admin, got %d", len(list.Items))
	}

	// 2. Create new admin via API
	newAdminReq := gen.CreateAdminRequest{
		Email:    "tech_new@example.org",
		FullName: "Technician User",
		Role:     gen.AdminRoleTechnician,
		Password: "password123456",
	}
	status, body = admin.do(t, h, http.MethodPost, "/v1/admins", newAdminReq)
	if status != http.StatusCreated {
		t.Fatalf("CreateAdmin status = %d, want 201: %s", status, string(body))
	}
	var enrolment gen.AdminEnrolment
	if err := json.Unmarshal(body, &enrolment); err != nil {
		t.Fatalf("unmarshal AdminEnrolment: %v", err)
	}
	if enrolment.Admin.Email != "tech_new@example.org" {
		t.Fatalf("enrolment email = %q, want tech_new@example.org", enrolment.Admin.Email)
	}
	if enrolment.Enrolment.TotpSecret == "" || enrolment.Enrolment.OtpauthUrl == "" {
		t.Fatal("expected TOTP secret and otpauthUrl in enrolment response")
	}
	if len(enrolment.RecoveryCodes.Codes) != 8 {
		t.Fatalf("expected 8 recovery codes, got %d", len(enrolment.RecoveryCodes.Codes))
	}

	createdID := enrolment.Admin.Id

	// 3. Get admin
	status, body = admin.do(t, h, http.MethodGet, "/v1/admins/"+createdID, nil)
	if status != http.StatusOK {
		t.Fatalf("GetAdmin status = %d, want 200: %s", status, string(body))
	}
	var fetched gen.Admin
	if err := json.Unmarshal(body, &fetched); err != nil {
		t.Fatalf("unmarshal Admin: %v", err)
	}
	if fetched.Id != createdID || fetched.FullName != "Technician User" {
		t.Fatalf("fetched admin mismatch: %+v", fetched)
	}

	// 4. Update admin (name, role, status)
	newRole := gen.AdminRoleViewer
	newStatus := gen.AdminStatusDisabled
	newName := "Technician Renamed"
	status, body = admin.do(t, h, http.MethodPatch, "/v1/admins/"+createdID, gen.UpdateAdminRequest{
		FullName: &newName,
		Role:     &newRole,
		Status:   &newStatus,
	})
	if status != http.StatusOK {
		t.Fatalf("UpdateAdmin status = %d, want 200: %s", status, string(body))
	}
	var updated gen.Admin
	if err := json.Unmarshal(body, &updated); err != nil {
		t.Fatalf("unmarshal updated Admin: %v", err)
	}
	if updated.FullName != newName || updated.Role != newRole || *updated.Status != newStatus {
		t.Fatalf("updated admin mismatch: %+v", updated)
	}

	// 5. Disabled admin cannot log in
	code, _ := totp.GenerateCode(enrolment.Enrolment.TotpSecret, time.Now())
	loginReq := gen.LoginRequest{
		Email:    "tech_new@example.org",
		Password: "password123456",
		TotpCode: &code,
	}
	status, _ = admin.do(t, h, http.MethodPost, "/v1/auth/login", loginReq)
	if status != http.StatusForbidden {
		t.Fatalf("login disabled admin status = %d, want 403", status)
	}
}

func TestAdminPasswordResetAndForcedChange(t *testing.T) {
	h := newTestHarness(t)
	superAdmin, _, _ := createAdminAndLogin(t, h, "admin_pw_reset@example.org", "Admin", "admin", "password123456")
	targetUser, targetSecret, _ := createAdminAndLogin(t, h, "target_user@example.org", "Target", "technician", "oldpassword123456")

	// Superadmin resets target user's password
	resetReq := gen.ResetAdminPasswordRequest{
		Password: "temporaryPassword123",
		Reason:   "User forgot password",
	}
	status, body := superAdmin.do(t, h, http.MethodPost, "/v1/admins/"+targetUser.id+"/reset-password", resetReq)
	if status != http.StatusNoContent {
		t.Fatalf("ResetAdminPassword status = %d, want 204: %s", status, string(body))
	}

	// Target user's old session was revoked
	status, _ = targetUser.do(t, h, http.MethodGet, "/v1/devices", nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("old session status = %d, want 401 after password reset", status)
	}

	// Target user logs in with the new temporary password
	code, err := totp.GenerateCode(targetSecret, time.Now())
	if err != nil {
		t.Fatalf("totp: %v", err)
	}

	loginResp := h.doJSON(t, http.MethodPost, "/v1/auth/login", "", gen.LoginRequest{
		Email:    "target_user@example.org",
		Password: "temporaryPassword123",
		TotpCode: &code,
	})
	defer loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want 200", loginResp.StatusCode)
	}

	var sessionCookie, csrfCookie *http.Cookie
	for _, c := range loginResp.Cookies() {
		if c.Name == "hdms_session" {
			sessionCookie = c
		}
		if c.Name == "hdms_csrf" {
			csrfCookie = c
		}
	}
	if sessionCookie == nil || csrfCookie == nil {
		t.Fatal("expected session and csrf cookies")
	}

	newTargetClient := adminClient{
		id:           targetUser.id,
		sessionToken: sessionCookie.Value,
		csrfToken:    csrfCookie.Value,
		client:       &http.Client{},
	}

	// Non-auth call must fail with 403 (password change required)
	status, body = newTargetClient.do(t, h, http.MethodGet, "/v1/devices", nil)
	if status != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 when password change required: %s", status, string(body))
	}
	if !strings.Contains(string(body), "password-change-required") {
		t.Fatalf("expected password-change-required problem, got: %s", string(body))
	}

	// Target user changes password
	status, body = newTargetClient.do(t, h, http.MethodPost, "/v1/auth/password", gen.ChangePasswordRequest{
		CurrentPassword: "temporaryPassword123",
		NewPassword:     "newBrandSecurePassword123",
	})
	if status != http.StatusNoContent {
		t.Fatalf("ChangeOwnPassword status = %d, want 204: %s", status, string(body))
	}

	// Now devices call must succeed
	status, body = newTargetClient.do(t, h, http.MethodGet, "/v1/devices", nil)
	if status != http.StatusOK {
		t.Fatalf("status after password change = %d, want 200: %s", status, string(body))
	}
}

func TestAdminForceTotpReenrolment(t *testing.T) {
	h := newTestHarness(t)
	superAdmin, _, _ := createAdminAndLogin(t, h, "admin_totp@example.org", "Admin", "admin", "password123456")
	targetUser, _, _ := createAdminAndLogin(t, h, "target_totp@example.org", "Target", "technician", "password123456")

	// Superadmin forces TOTP reset
	status, body := superAdmin.do(t, h, http.MethodPost, "/v1/admins/"+targetUser.id+"/reset-totp", gen.ReasonRequest{
		Reason: "Phone lost",
	})
	if status != http.StatusOK {
		t.Fatalf("ForceAdminTotpReenrolment status = %d, want 200: %s", status, string(body))
	}
	var newSecretEnrolment gen.TotpEnrolment
	if err := json.Unmarshal(body, &newSecretEnrolment); err != nil {
		t.Fatalf("unmarshal TotpEnrolment: %v", err)
	}

	// Target user logs in with new handed-over secret
	code, err := totp.GenerateCode(newSecretEnrolment.TotpSecret, time.Now())
	if err != nil {
		t.Fatalf("generate code: %v", err)
	}

	loginResp := h.doJSON(t, http.MethodPost, "/v1/auth/login", "", gen.LoginRequest{
		Email:    "target_totp@example.org",
		Password: "password123456",
		TotpCode: &code,
	})
	defer loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want 200", loginResp.StatusCode)
	}

	var sessionCookie, csrfCookie *http.Cookie
	for _, c := range loginResp.Cookies() {
		if c.Name == "hdms_session" {
			sessionCookie = c
		}
		if c.Name == "hdms_csrf" {
			csrfCookie = c
		}
	}

	newTargetClient := adminClient{
		id:           targetUser.id,
		sessionToken: sessionCookie.Value,
		csrfToken:    csrfCookie.Value,
		client:       &http.Client{},
	}

	// Non-auth call must fail with 403 (totp re-enrolment required)
	status, body = newTargetClient.do(t, h, http.MethodGet, "/v1/devices", nil)
	if status != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 when TOTP re-enrolment required: %s", status, string(body))
	}
	if !strings.Contains(string(body), "totp-reenrolment-required") {
		t.Fatalf("expected totp-reenrolment-required problem, got: %s", string(body))
	}

	// Begin TOTP re-enrolment
	status, body = newTargetClient.do(t, h, http.MethodPost, "/v1/auth/totp/reenrol", nil)
	if status != http.StatusOK {
		t.Fatalf("BeginTotpReenrolment status = %d, want 200: %s", status, string(body))
	}
	var selfEnrol gen.TotpEnrolment
	if err := json.Unmarshal(body, &selfEnrol); err != nil {
		t.Fatalf("unmarshal TotpEnrolment: %v", err)
	}

	// Confirm with code from self-enrolled secret
	confirmCode, _ := totp.GenerateCode(selfEnrol.TotpSecret, time.Now())
	status, body = newTargetClient.do(t, h, http.MethodPost, "/v1/auth/totp/confirm", gen.ConfirmTotpRequest{
		TotpCode: confirmCode,
	})
	if status != http.StatusNoContent {
		t.Fatalf("ConfirmTotpReenrolment status = %d, want 204: %s", status, string(body))
	}

	// Devices call now succeeds
	status, body = newTargetClient.do(t, h, http.MethodGet, "/v1/devices", nil)
	if status != http.StatusOK {
		t.Fatalf("status after TOTP confirm = %d, want 200: %s", status, string(body))
	}
}

func TestSelfServiceRecoveryCodes(t *testing.T) {
	h := newTestHarness(t)
	admin, _, _ := createAdminAndLogin(t, h, "admin_rc@example.org", "Admin", "admin", "password123456")

	// Regenerate recovery codes
	status, body := admin.do(t, h, http.MethodPost, "/v1/auth/recovery-codes", nil)
	if status != http.StatusOK {
		t.Fatalf("RegenerateRecoveryCodes status = %d, want 200: %s", status, string(body))
	}
	var rcResp gen.RecoveryCodes
	if err := json.Unmarshal(body, &rcResp); err != nil {
		t.Fatalf("unmarshal RecoveryCodes: %v", err)
	}
	if len(rcResp.Codes) != 8 {
		t.Fatalf("expected 8 codes, got %d", len(rcResp.Codes))
	}

	codeToUse := rcResp.Codes[0]

	// Login with recovery code instead of TOTP
	loginResp := h.doJSON(t, http.MethodPost, "/v1/auth/login", "", gen.LoginRequest{
		Email:        "admin_rc@example.org",
		Password:     "password123456",
		RecoveryCode: &codeToUse,
	})
	defer loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("login with recovery code status = %d, want 200", loginResp.StatusCode)
	}

	// Second attempt with the SAME recovery code must fail
	loginResp2 := h.doJSON(t, http.MethodPost, "/v1/auth/login", "", gen.LoginRequest{
		Email:        "admin_rc@example.org",
		Password:     "password123456",
		RecoveryCode: &codeToUse,
	})
	defer loginResp2.Body.Close()
	if loginResp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("second login with same recovery code status = %d, want 401", loginResp2.StatusCode)
	}
}

func TestConcurrentRecoveryCodeRedemption(t *testing.T) {
	h := newTestHarness(t)
	_, _, recoveryCodes := createAdminAndLogin(t, h, "admin_concurrent@example.org", "Admin", "admin", "password123456")

	code := recoveryCodes[0]

	var wg sync.WaitGroup
	var successCount int32
	var failCount int32

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp := h.doJSON(t, http.MethodPost, "/v1/auth/login", "", gen.LoginRequest{
				Email:        "admin_concurrent@example.org",
				Password:     "password123456",
				RecoveryCode: &code,
			})
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				atomic.AddInt32(&successCount, 1)
			} else {
				atomic.AddInt32(&failCount, 1)
			}
		}()
	}
	wg.Wait()

	if successCount != 1 || failCount != 1 {
		t.Fatalf("concurrent recovery code redemption: success = %d, fail = %d; want exactly 1 success and 1 fail", successCount, failCount)
	}
}

func TestAccountLockoutAndUnlock(t *testing.T) {
	h := newTestHarness(t)
	superAdmin, _, _ := createAdminAndLogin(t, h, "super_unlock@example.org", "Super", "admin", "password123456")
	victim, victimSecret, _ := createAdminAndLogin(t, h, "victim@example.org", "Victim", "viewer", "password123456")

	// Trigger 5 failed password attempts
	for i := 1; i <= 5; i++ {
		resp := h.doJSON(t, http.MethodPost, "/v1/auth/login", "", gen.LoginRequest{
			Email:    "victim@example.org",
			Password: "wrongPassword123",
		})
		resp.Body.Close()
	}

	// 6th attempt with CORRECT password and TOTP should fail with 403 (Account locked)
	validCode, _ := totp.GenerateCode(victimSecret, time.Now())
	resp := h.doJSON(t, http.MethodPost, "/v1/auth/login", "", gen.LoginRequest{
		Email:    "victim@example.org",
		Password: "password123456",
		TotpCode: &validCode,
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status during lockout = %d, want 403", resp.StatusCode)
	}

	// Superadmin unlocks victim account via API
	status, body := superAdmin.do(t, h, http.MethodPost, "/v1/admins/"+victim.id+"/unlock", gen.ReasonRequest{
		Reason: "Admin unlocked after identity verification",
	})
	if status != http.StatusNoContent {
		t.Fatalf("UnlockAdmin status = %d, want 204: %s", status, string(body))
	}

	// Login succeeds now
	validCode2, _ := totp.GenerateCode(victimSecret, time.Now())
	resp2 := h.doJSON(t, http.MethodPost, "/v1/auth/login", "", gen.LoginRequest{
		Email:    "victim@example.org",
		Password: "password123456",
		TotpCode: &validCode2,
	})
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("status after unlock = %d, want 200", resp2.StatusCode)
	}

	// Test CLI unlock
	// Lock victim again
	for i := 1; i <= 5; i++ {
		r := h.doJSON(t, http.MethodPost, "/v1/auth/login", "", gen.LoginRequest{
			Email:    "victim@example.org",
			Password: "wrongPassword123",
		})
		r.Body.Close()
	}

	// CLI unlock
	if err := h.auth.UnlockAdminByEmail(context.Background(), "victim@example.org"); err != nil {
		t.Fatalf("UnlockAdminByEmail: %v", err)
	}

	// Login succeeds again
	validCode3, _ := totp.GenerateCode(victimSecret, time.Now())
	resp3 := h.doJSON(t, http.MethodPost, "/v1/auth/login", "", gen.LoginRequest{
		Email:    "victim@example.org",
		Password: "password123456",
		TotpCode: &validCode3,
	})
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusOK {
		t.Fatalf("status after CLI unlock = %d, want 200", resp3.StatusCode)
	}
}

func TestAuthAuditTrailNoPlaintextSecrets(t *testing.T) {
	h := newTestHarness(t)

	superAdmin, _, _ := createAdminAndLogin(t, h, "super_audit@example.org", "Super", "admin", "password123456")

	// Create new admin
	newReq := gen.CreateAdminRequest{
		Email:    "audited_user@example.org",
		FullName: "Audited Admin",
		Role:     gen.AdminRoleTechnician,
		Password: "password123456",
	}
	_, body := superAdmin.do(t, h, http.MethodPost, "/v1/admins", newReq)
	var enrol gen.AdminEnrolment
	_ = json.Unmarshal(body, &enrol)
	createdID := enrol.Admin.Id

	// Superadmin resets password
	superAdmin.do(t, h, http.MethodPost, "/v1/admins/"+createdID+"/reset-password", gen.ResetAdminPasswordRequest{
		Password: "newPassword123456",
		Reason:   "Routine rotation",
	})

	// Check audit events directly from DB
	ctx := context.Background()
	events, err := h.audit.List(ctx, auditapi.ListParams{Limit: 100})
	if err != nil {
		t.Fatalf("audit.List: %v", err)
	}

	// Assert no plaintext secrets in any audit event
	sensitiveStrings := []string{
		"newPassword123456",
		"password123456",
		enrol.Enrolment.TotpSecret,
	}
	for _, rc := range enrol.RecoveryCodes.Codes {
		sensitiveStrings = append(sensitiveStrings, rc)
	}

	for _, ev := range events {
		payloadBytes, _ := json.Marshal(ev.Payload)
		payloadStr := string(payloadBytes)
		for _, secret := range sensitiveStrings {
			if secret != "" && strings.Contains(payloadStr, secret) {
				t.Fatalf("SECURITY VIOLATION: secret %q leaked into audit event %s payload: %s", secret, ev.Action, payloadStr)
			}
		}
	}
}

func TestAdminCanSetTheirOwnLocale(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	admin, _, _ := createAdminAndLogin(t, h, "admin_locale@example.org", "Locale Admin", "admin", "password123456")

	status, body := admin.do(t, h, http.MethodGet, "/v1/auth/me", nil)
	if status != http.StatusOK {
		t.Fatalf("GET /v1/auth/me status = %d, want 200: %s", status, string(body))
	}
	var me gen.Admin
	if err := json.Unmarshal(body, &me); err != nil {
		t.Fatalf("unmarshal me: %v", err)
	}
	if me.Locale == nil || string(*me.Locale) != "ja" {
		t.Fatalf("a new account defaults to Japanese, got %v", me.Locale)
	}

	status, body = admin.do(t, h, http.MethodPatch, "/v1/auth/me/locale", map[string]string{"locale": "en"})
	if status != http.StatusOK {
		t.Fatalf("PATCH /v1/auth/me/locale status = %d, want 200: %s", status, string(body))
	}
	var updated gen.Admin
	if err := json.Unmarshal(body, &updated); err != nil {
		t.Fatalf("unmarshal updated: %v", err)
	}
	if updated.Locale == nil || string(*updated.Locale) != "en" {
		t.Fatalf("updated locale = %v, want 'en'", updated.Locale)
	}

	status, body = admin.do(t, h, http.MethodGet, "/v1/auth/me", nil)
	if status != http.StatusOK {
		t.Fatalf("GET /v1/auth/me status = %d, want 200: %s", status, string(body))
	}
	var meAfter gen.Admin
	if err := json.Unmarshal(body, &meAfter); err != nil {
		t.Fatalf("unmarshal meAfter: %v", err)
	}
	if meAfter.Locale == nil || string(*meAfter.Locale) != "en" {
		t.Fatalf("meAfter locale = %v, want 'en'", meAfter.Locale)
	}
}

func TestAdminLocaleRejectsAnUnsupportedValue(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	admin, _, _ := createAdminAndLogin(t, h, "admin_badlocale@example.org", "Bad Locale Admin", "admin", "password123456")

	status, body := admin.do(t, h, http.MethodPatch, "/v1/auth/me/locale", map[string]string{"locale": "de"})
	if status != http.StatusBadRequest {
		t.Fatalf("PATCH /v1/auth/me/locale with unsupported locale status = %d, want 400: %s", status, string(body))
	}
}

func TestAdminLocaleIsScopedToTheCallersOwnAccount(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	operator, _, _ := createAdminAndLogin(t, h, "operator_scope@example.org", "Operator Admin", "admin", "password123456")
	other, _, _ := createAdminAndLogin(t, h, "other@example.test", "Other Admin", "admin", "password123456")

	status, body := operator.do(t, h, http.MethodPatch, "/v1/auth/me/locale", map[string]string{"locale": "en"})
	if status != http.StatusOK {
		t.Fatalf("PATCH /v1/auth/me/locale status = %d, want 200: %s", status, string(body))
	}

	status, body = operator.do(t, h, http.MethodGet, "/v1/admins/"+other.id, nil)
	if status != http.StatusOK {
		t.Fatalf("GET /v1/admins/%s status = %d, want 200: %s", other.id, status, string(body))
	}
	var fetchedOther gen.Admin
	if err := json.Unmarshal(body, &fetchedOther); err != nil {
		t.Fatalf("unmarshal fetchedOther: %v", err)
	}
	if fetchedOther.Locale == nil || string(*fetchedOther.Locale) != "ja" {
		t.Fatalf("setting my language must not touch anyone else's, got %v", fetchedOther.Locale)
	}
}
