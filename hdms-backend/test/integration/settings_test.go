//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/catalog/catalogapi"
	"github.com/hito-hospital/hdms/internal/modules/checkout/checkoutapi"
	"github.com/hito-hospital/hdms/internal/modules/credentials/credentialsapi"
	"github.com/hito-hospital/hdms/internal/modules/identity/identityapi"
	"github.com/hito-hospital/hdms/internal/modules/lending/lendingapi"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
)


// TestSettingsGetAndUpdate tests fetching and updating policy and templates.
func TestSettingsGetAndUpdate(t *testing.T) {
	h := newTestHarness(t)

	// 1. GET /v1/settings returns initial defaults
	resp := h.doJSON(t, http.MethodGet, "/v1/settings", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /v1/settings status = %d, want 200", resp.StatusCode)
	}
	var st gen.Settings
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		t.Fatalf("decode settings: %v", err)
	}
	resp.Body.Close()

	if st.Policy.BlockOnOverdue != false {
		t.Errorf("default blockOnOverdue = %v, want false", st.Policy.BlockOnOverdue)
	}
	if st.Policy.LowStockThreshold != 10 {
		t.Errorf("default lowStockThreshold = %d, want 10", st.Policy.LowStockThreshold)
	}
	if st.Policy.PaperBacklogHours != 48 {
		t.Errorf("default paperBacklogHours = %d, want 48", st.Policy.PaperBacklogHours)
	}
	if st.LabelTemplate.SheetWidthMm != 210 || st.LabelTemplate.SheetHeightMm != 297 {
		t.Errorf("default sheet size = %vx%v, want 210x297", st.LabelTemplate.SheetWidthMm, st.LabelTemplate.SheetHeightMm)
	}
	if st.SlipTemplate.HospitalName != "HITO HOSPITAL" {
		t.Errorf("default hospital name = %q, want 'HITO HOSPITAL'", st.SlipTemplate.HospitalName)
	}

	// 2. PATCH /v1/settings updates policy and templates
	updateReq := gen.UpdateSettingsRequest{
		Policy: &gen.PolicySettings{
			BlockOnOverdue:            true,
			SessionIdleTimeoutSeconds: 60,
			KioskSoundEnabled:         false,
			LowStockThreshold:         15,
			PaperBacklogHours:         24,
		},
		LabelTemplate: &gen.LabelTemplateSettings{
			SheetWidthMm:  210,
			SheetHeightMm: 297,
			Columns:       4,
			Rows:          10,
			MarginTopMm:   10,
			MarginLeftMm:  10,
			GutterXMm:     2,
			GutterYMm:     2,
			LabelWidthMm:  45,
			LabelHeightMm: 25,
		},
		SlipTemplate: &gen.SlipTemplateSettings{
			HospitalName:  "HITO CENTRAL CLINIC",
			PageRefFormat: "REF-YYYY-MM-pNN",
			RowsPerPage:   25,
			Columns:       []string{"#", "TAG", "NAME", "EMPID", "OUT", "IN"},
		},
	}

	patchResp := h.doJSON(t, http.MethodPatch, "/v1/settings", h.csrfToken, updateReq)
	if patchResp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH /v1/settings status = %d, want 200", patchResp.StatusCode)
	}
	var updated gen.Settings
	if err := json.NewDecoder(patchResp.Body).Decode(&updated); err != nil {
		t.Fatalf("decode updated settings: %v", err)
	}
	patchResp.Body.Close()

	if !updated.Policy.BlockOnOverdue || updated.Policy.SessionIdleTimeoutSeconds != 60 || updated.Policy.LowStockThreshold != 15 {
		t.Errorf("updated policy mismatch: %+v", updated.Policy)
	}
	if updated.LabelTemplate.Columns != 4 || updated.LabelTemplate.Rows != 10 {
		t.Errorf("updated label template mismatch: %+v", updated.LabelTemplate)
	}
	if updated.SlipTemplate.HospitalName != "HITO CENTRAL CLINIC" || len(updated.SlipTemplate.Columns) != 6 {
		t.Errorf("updated slip template mismatch: %+v", updated.SlipTemplate)
	}

	// 3. Verify audit log entry for settings.updated
	var auditCount int
	var payloadBytes []byte
	err := h.pool.QueryRow(context.Background(), `
		SELECT count(*), payload
		FROM audit_events
		WHERE action = 'settings.updated'
		GROUP BY payload
		ORDER BY max(at) DESC LIMIT 1
	`).Scan(&auditCount, &payloadBytes)
	if err != nil {
		t.Fatalf("query audit_events for settings.updated: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		t.Fatalf("decode audit payload: %v", err)
	}
	if payload["before"] == nil || payload["after"] == nil {
		t.Errorf("audit payload missing before/after: %v", payload)
	}
}

// TestSettingsValidationRefusesOutOfRangeValues tests rejection of bad values.
func TestSettingsValidationRefusesOutOfRangeValues(t *testing.T) {
	h := newTestHarness(t)

	// Zero / negative timeout
	badReq := gen.UpdateSettingsRequest{
		Policy: &gen.PolicySettings{
			BlockOnOverdue:            true,
			SessionIdleTimeoutSeconds: 0,
			KioskSoundEnabled:         true,
			LowStockThreshold:         10,
			PaperBacklogHours:         48,
		},
	}
	resp := h.doJSON(t, http.MethodPatch, "/v1/settings", h.csrfToken, badReq)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("zero timeout status = %d, want 422", resp.StatusCode)
	}
	resp.Body.Close()

	// Negative lowStockThreshold
	badReq2 := gen.UpdateSettingsRequest{
		Policy: &gen.PolicySettings{
			BlockOnOverdue:            true,
			SessionIdleTimeoutSeconds: 45,
			KioskSoundEnabled:         true,
			LowStockThreshold:         -5,
			PaperBacklogHours:         48,
		},
	}
	resp2 := h.doJSON(t, http.MethodPatch, "/v1/settings", h.csrfToken, badReq2)
	if resp2.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("negative threshold status = %d, want 422", resp2.StatusCode)
	}
	resp2.Body.Close()
}

// TestPolicyChangeTakesEffectOnNextCheckoutWithoutRestart tests that
// toggling blockOnOverdue immediately controls borrow permissions.
func TestPolicyChangeTakesEffectOnNextCheckoutWithoutRestart(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	// 1. Create a user and two devices
	u, err := h.identity.CreateUser(ctx, identityapi.CreateUserParams{
		EmployeeNo:   "E9001",
		FullName:     "Policy Test User",
		RegisteredBy: "admin:test",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	userCard, err := h.credentials.Issue(ctx, credentialsapi.IssueParams{
		SubjectType: credentialsapi.SubjectUser,
		SubjectID:   u.ID,
		Kind:        credentialsapi.KindQR,
		IssuedBy:    "admin:test",
	})
	if err != nil {
		t.Fatalf("issue user card: %v", err)
	}

	cat, err := h.catalog.CreateCategory(ctx, catalogapi.CreateCategoryParams{
		Name: "Test Category",
	})
	if err != nil {
		t.Fatalf("create category: %v", err)
	}

	dev1, err := h.catalog.CreateDevice(ctx, catalogapi.CreateDeviceParams{
		AssetTag:   "DEV-POL-1",
		Name:       "Test Device 1",
		CategoryID: cat.ID,
	}, "admin:test")
	if err != nil {
		t.Fatalf("create device 1: %v", err)
	}

	dev2, err := h.catalog.CreateDevice(ctx, catalogapi.CreateDeviceParams{
		AssetTag:   "DEV-POL-2",
		Name:       "Test Device 2",
		CategoryID: cat.ID,
	}, "admin:test")
	if err != nil {
		t.Fatalf("create device 2: %v", err)
	}
	dev2Cred, err := h.credentials.Issue(ctx, credentialsapi.IssueParams{
		SubjectType: credentialsapi.SubjectDevice,
		SubjectID:   dev2.ID,
		Kind:        credentialsapi.KindQR,
		IssuedBy:    "admin:test",
	})
	if err != nil {
		t.Fatalf("issue dev2 cred: %v", err)
	}


	// 2. Open an overdue loan for the user (using RecordHistorical with past due_at / borrowed_at)
	yesterday := time.Now().Add(-24 * time.Hour)
	pastDue := time.Now().Add(-2 * time.Hour)
	_, err = h.lending.OpenLoan(ctx, dev1.ID, u.ID, &pastDue, lendingapi.OpenMeta{
		Actor:      "admin:test",
		Source:     "scanner",
		BorrowedAt: yesterday,
	})
	if err != nil {
		// OpenLoan rejects backdating more than 5s; use RecordHistorical
		_, err = h.lending.RecordHistorical(ctx, lendingapi.RecordHistoricalParams{
			DeviceID:     dev1.ID,
			UserID:       u.ID,
			Origin:       lendingapi.OriginPaper,
			BorrowedAt:   yesterday,
			BorrowActor:  "admin:test",
			BorrowSource: "paper",
			PaperRef:     "PAGE-1",
			RecordedAt:   &yesterday,
			RecordedBy:   "admin:test",
		})
		if err != nil {
			t.Fatalf("record historical overdue loan: %v", err)
		}
		// Set due_at in the past
		_, _ = h.pool.Exec(ctx, `UPDATE loans SET due_at = $2 WHERE device_id = $1 AND status = 'open'`, dev1.ID, pastDue)
	}

	// 3. Register a kiosk and open a session
	kioskID, _, err := h.auth.RegisterKiosk(ctx, "Test Kiosk Policy", "Room A")
	if err != nil {
		t.Fatalf("register kiosk: %v", err)
	}

	session, err := h.checkout.CreateSession(ctx, checkoutapi.CreateSessionParams{
		KioskID: kioskID,
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	// With default blockOnOverdue=false: user identifies then borrows dev2 -> succeeds
	scan1, err := h.checkout.Scan(ctx, checkoutapi.ScanParams{
		SessionID: session.ID,
		Token:     userCard.Token,
		Actor:     "kiosk:" + kioskID,
		Source:    "scanner",
	})
	if err != nil {
		t.Fatalf("scan user card: %v", err)
	}
	if scan1.Outcome.Kind != checkoutapi.OutcomeUserIdentified {
		t.Fatalf("scan user outcome = %s, want user_identified", scan1.Outcome.Kind)
	}

	scan2, err := h.checkout.Scan(ctx, checkoutapi.ScanParams{
		SessionID: session.ID,
		Token:     dev2Cred.Token,
		Actor:     "kiosk:" + kioskID,
		Source:    "scanner",
	})
	if err != nil {
		t.Fatalf("scan dev2 cred: %v", err)
	}
	if scan2.Outcome.Kind != checkoutapi.OutcomeBorrowed {
		t.Fatalf("scan dev2 outcome = %s, want borrowed (when blockOnOverdue=false)", scan2.Outcome.Kind)
	}

	// Return dev2 so it is available again
	_, err = h.lending.CloseLoan(ctx, scan2.Outcome.LoanID, lendingapi.CloseMeta{
		Actor:  "kiosk:" + kioskID,
		Source: "scanner",
	})
	if err != nil {
		t.Fatalf("close dev2 loan: %v", err)
	}
	_, err = h.catalog.SetStatus(ctx, dev2.ID, catalogapi.StatusAvailable, "", "admin:test")
	if err != nil {
		t.Fatalf("set dev2 available: %v", err)
	}


	// 4. Enable blockOnOverdue via PATCH /v1/settings
	updateReq := gen.UpdateSettingsRequest{
		Policy: &gen.PolicySettings{
			BlockOnOverdue:            true,
			SessionIdleTimeoutSeconds: 45,
			KioskSoundEnabled:         true,
			LowStockThreshold:         10,
			PaperBacklogHours:         48,
		},
	}
	patchResp := h.doJSON(t, http.MethodPatch, "/v1/settings", h.csrfToken, updateReq)
	if patchResp.StatusCode != http.StatusOK {
		t.Fatalf("enable blockOnOverdue status = %d, want 200", patchResp.StatusCode)
	}
	patchResp.Body.Close()

	// 5. Open a new session and try to borrow dev2 again -> must be rejected!
	session2, err := h.checkout.CreateSession(ctx, checkoutapi.CreateSessionParams{
		KioskID: kioskID,
	})
	if err != nil {
		t.Fatalf("create session2: %v", err)
	}

	_, err = h.checkout.Scan(ctx, checkoutapi.ScanParams{
		SessionID: session2.ID,
		Token:     userCard.Token,
		Actor:     "kiosk:" + kioskID,
		Source:    "scanner",
	})
	if err != nil {
		t.Fatalf("scan user card session2: %v", err)
	}

	scan4, err := h.checkout.Scan(ctx, checkoutapi.ScanParams{
		SessionID: session2.ID,
		Token:     dev2Cred.Token,
		Actor:     "kiosk:" + kioskID,
		Source:    "scanner",
	})
	if err != nil {
		t.Fatalf("scan dev2 cred session2: %v", err)
	}
	if scan4.Outcome.Kind != checkoutapi.OutcomeRejected {
		t.Fatalf("scan dev2 outcome = %s, want rejected (when blockOnOverdue=true and user has overdue loan)", scan4.Outcome.Kind)
	}
}

// TestKioskManagementLifecycle tests 4.10b kiosk endpoints:
// GET, PATCH, Enable, Disable, RotateToken, and pairing code.
func TestKioskManagementLifecycle(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	// 1. Create kiosk
	createReq := gen.CreateKioskRequest{
		Name:     "Kiosk Ward 3",
		Location: strPtr("Ward 3 Entrance"),
	}
	createResp := h.doJSON(t, http.MethodPost, "/v1/kiosks", h.csrfToken, createReq)
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /v1/kiosks status = %d, want 201", createResp.StatusCode)
	}
	var created gen.KioskWithToken
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode created kiosk: %v", err)
	}
	createResp.Body.Close()

	if created.Token == "" {
		t.Fatalf("expected plaintext token on kiosk creation")
	}

	// 2. GET /v1/kiosks/{id}
	getResp := h.doJSON(t, http.MethodGet, "/v1/kiosks/"+created.Id, "", nil)
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /v1/kiosks/{id} status = %d, want 200", getResp.StatusCode)
	}
	var fetched gen.Kiosk
	if err := json.NewDecoder(getResp.Body).Decode(&fetched); err != nil {
		t.Fatalf("decode fetched kiosk: %v", err)
	}
	getResp.Body.Close()
	if fetched.Name != "Kiosk Ward 3" || fetched.Status != gen.KioskStatusActive {
		t.Errorf("fetched kiosk mismatch: %+v", fetched)
	}

	// 3. PATCH /v1/kiosks/{id} (edit name, location, enabledSources)
	newSources := []string{"scanner", "camera"}
	updateReq := gen.UpdateKioskRequest{
		Name:           strPtr("Kiosk Ward 3B"),
		Location:       strPtr("Ward 3B Desk"),
		EnabledSources: &newSources,
	}
	patchResp := h.doJSON(t, http.MethodPatch, "/v1/kiosks/"+created.Id, h.csrfToken, updateReq)
	if patchResp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH /v1/kiosks/{id} status = %d, want 200", patchResp.StatusCode)
	}
	var patched gen.Kiosk
	if err := json.NewDecoder(patchResp.Body).Decode(&patched); err != nil {
		t.Fatalf("decode patched kiosk: %v", err)
	}
	patchResp.Body.Close()

	if patched.Name != "Kiosk Ward 3B" || *patched.Location != "Ward 3B Desk" {
		t.Errorf("patched kiosk details mismatch: %+v", patched)
	}
	if len(patched.EnabledSources) != 2 || patched.EnabledSources[1] != "camera" {
		t.Errorf("patched enabled sources mismatch: %+v", patched.EnabledSources)
	}

	// 4. Rotate token
	rotateResp := h.doJSON(t, http.MethodPost, "/v1/kiosks/"+created.Id+"/rotate-token", h.csrfToken, nil)
	if rotateResp.StatusCode != http.StatusOK {
		t.Fatalf("POST /v1/kiosks/{id}/rotate-token status = %d, want 200", rotateResp.StatusCode)
	}
	var rotated gen.KioskWithToken
	if err := json.NewDecoder(rotateResp.Body).Decode(&rotated); err != nil {
		t.Fatalf("decode rotated kiosk: %v", err)
	}
	rotateResp.Body.Close()

	if rotated.Token == "" || rotated.Token == created.Token {
		t.Fatalf("rotated token should be fresh and non-empty")
	}

	// Old token is invalidated
	_, err := h.auth.ValidateKioskToken(ctx, created.Token)
	if err == nil {
		t.Fatalf("expected old token to be rejected after rotation")
	}
	// New token authenticates
	_, err = h.auth.ValidateKioskToken(ctx, rotated.Token)
	if err != nil {
		t.Fatalf("expected new token to authenticate: %v", err)
	}

	// 5. Disable kiosk
	disableResp := h.doJSON(t, http.MethodPost, "/v1/kiosks/"+created.Id+"/disable", h.csrfToken, nil)
	if disableResp.StatusCode != http.StatusOK {
		t.Fatalf("POST /v1/kiosks/{id}/disable status = %d, want 200", disableResp.StatusCode)
	}
	var disabled gen.Kiosk
	if err := json.NewDecoder(disableResp.Body).Decode(&disabled); err != nil {
		t.Fatalf("decode disabled kiosk: %v", err)
	}
	disableResp.Body.Close()
	if disabled.Status != gen.KioskStatusDisabled {
		t.Errorf("kiosk status = %s, want disabled", disabled.Status)
	}

	// Disabled kiosk token is refused
	_, err = h.auth.ValidateKioskToken(ctx, rotated.Token)
	if err == nil {
		t.Fatalf("expected disabled kiosk token to be rejected")
	}

	// 6. Enable kiosk
	enableResp := h.doJSON(t, http.MethodPost, "/v1/kiosks/"+created.Id+"/enable", h.csrfToken, nil)
	if enableResp.StatusCode != http.StatusOK {
		t.Fatalf("POST /v1/kiosks/{id}/enable status = %d, want 200", enableResp.StatusCode)
	}
	var reEnabled gen.Kiosk
	if err := json.NewDecoder(enableResp.Body).Decode(&reEnabled); err != nil {
		t.Fatalf("decode re-enabled kiosk: %v", err)
	}
	enableResp.Body.Close()
	if reEnabled.Status != gen.KioskStatusActive {
		t.Errorf("kiosk status = %s, want active", reEnabled.Status)
	}

	// Token works again once re-enabled
	_, err = h.auth.ValidateKioskToken(ctx, rotated.Token)
	if err != nil {
		t.Fatalf("expected re-enabled kiosk token to authenticate: %v", err)
	}

}

func TestKioskDefaultLocaleRoundTrips(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)

	// Create kiosk
	createReq := gen.CreateKioskRequest{
		Name: "Ward 3 counter",
	}
	createResp := h.doJSON(t, http.MethodPost, "/v1/kiosks", h.csrfToken, createReq)
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /v1/kiosks status = %d, want 201", createResp.StatusCode)
	}
	var created gen.KioskWithToken
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode created kiosk: %v", err)
	}
	createResp.Body.Close()
	if created.DefaultLocale != gen.KioskWithTokenDefaultLocaleJa {
		t.Fatalf("a new kiosk defaults to Japanese, got %v", created.DefaultLocale)
	}

	// Update kiosk defaultLocale to "en"
	enLocale := gen.UpdateKioskRequestDefaultLocaleEn
	updateReq := gen.UpdateKioskRequest{
		DefaultLocale: &enLocale,
	}
	patchResp := h.doJSON(t, http.MethodPatch, "/v1/kiosks/"+created.Id, h.csrfToken, updateReq)
	if patchResp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH /v1/kiosks status = %d, want 200", patchResp.StatusCode)
	}
	var patched gen.Kiosk
	if err := json.NewDecoder(patchResp.Body).Decode(&patched); err != nil {
		t.Fatalf("decode patched kiosk: %v", err)
	}
	patchResp.Body.Close()
	if patched.DefaultLocale != gen.KioskDefaultLocaleEn {
		t.Fatalf("patched defaultLocale = %v, want 'en'", patched.DefaultLocale)
	}

	// Fetch kiosk and verify defaultLocale is "en"
	getResp := h.doJSON(t, http.MethodGet, "/v1/kiosks/"+created.Id, "", nil)
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /v1/kiosks status = %d, want 200", getResp.StatusCode)
	}
	var fetched gen.Kiosk
	if err := json.NewDecoder(getResp.Body).Decode(&fetched); err != nil {
		t.Fatalf("decode fetched kiosk: %v", err)
	}
	getResp.Body.Close()
	if fetched.DefaultLocale != gen.KioskDefaultLocaleEn {
		t.Fatalf("fetched defaultLocale = %v, want 'en'", fetched.DefaultLocale)
	}
}

func TestPairingResponseCarriesTheKioskDefaultLocale(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)

	// Create kiosk and update to "en"
	createReq := gen.CreateKioskRequest{Name: "Ward 4 counter"}
	createResp := h.doJSON(t, http.MethodPost, "/v1/kiosks", h.csrfToken, createReq)
	var created gen.KioskWithToken
	json.NewDecoder(createResp.Body).Decode(&created)
	createResp.Body.Close()

	enLocale := gen.UpdateKioskRequestDefaultLocaleEn
	patchResp := h.doJSON(t, http.MethodPatch, "/v1/kiosks/"+created.Id, h.csrfToken, gen.UpdateKioskRequest{DefaultLocale: &enLocale})
	patchResp.Body.Close()

	// Issue pairing code
	codeResp := h.doJSON(t, http.MethodPost, "/v1/kiosks/"+created.Id+"/pairing-code", h.csrfToken, nil)
	var code gen.KioskPairingCode
	json.NewDecoder(codeResp.Body).Decode(&code)
	codeResp.Body.Close()

	// Redeem pairing code
	pairResp := h.doJSON(t, http.MethodPost, "/v1/kiosks/pair", "", gen.PairKioskRequest{Code: code.Code})
	if pairResp.StatusCode != http.StatusOK {
		t.Fatalf("POST /v1/kiosks/pair status = %d, want 200", pairResp.StatusCode)
	}
	var paired gen.PairKioskResponse
	json.NewDecoder(pairResp.Body).Decode(&paired)
	pairResp.Body.Close()

	if paired.DefaultLocale != gen.PairKioskResponseDefaultLocaleEn {
		t.Fatalf("paired defaultLocale = %v, want 'en'", paired.DefaultLocale)
	}
}

func TestRejectsAnUnsupportedLocale(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)

	createReq := gen.CreateKioskRequest{Name: "Ward 5 counter"}
	createResp := h.doJSON(t, http.MethodPost, "/v1/kiosks", h.csrfToken, createReq)
	var created gen.KioskWithToken
	json.NewDecoder(createResp.Body).Decode(&created)
	createResp.Body.Close()

	deLocale := gen.UpdateKioskRequestDefaultLocale("de")
	patchResp := h.doJSON(t, http.MethodPatch, "/v1/kiosks/"+created.Id, h.csrfToken, gen.UpdateKioskRequest{DefaultLocale: &deLocale})
	defer patchResp.Body.Close()
	if patchResp.StatusCode != http.StatusBadRequest && patchResp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("PATCH with unsupported locale status = %d, want 400 or 422", patchResp.StatusCode)
	}
}

func strPtr(s string) *string {
	return &s
}
