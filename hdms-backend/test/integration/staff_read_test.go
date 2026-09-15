//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/lending/lendingapi"
)

// The privacy rule, enforced in the handler and not in the client: a staff
// member learns that a device is out and when it is due, never who has it.
func TestTheStaffDeviceListNeverNamesTheBorrower(t *testing.T) {
	env := newHTTPTestEnv(t)
	borrower := env.SeedUser(t, "E-BORROW-1", "Dr Borrower", "borrower@hospital.example")
	device := env.SeedDevice(t, "AT-STAFF-1", "Projector")
	env.SeedOpenLoan(t, device.ID, borrower.ID)

	req := httptest.NewRequest(http.MethodGet, "/v1/staff/devices", nil)
	req.AddCookie(&http.Cookie{Name: "hdms_staff_session", Value: env.StaffSessionToken})
	rec := httptest.NewRecorder()
	env.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/staff/devices = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "Dr Borrower") || strings.Contains(body, borrower.ID) {
		t.Fatalf("the staff device list disclosed the borrower: %s", body)
	}

	var payload struct {
		Items []struct {
			AssetTag       string `json:"assetTag"`
			Availability   string `json:"availability"`
			ExpectedBackAt string `json:"expectedBackAt"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var found bool
	for _, item := range payload.Items {
		if item.AssetTag == "AT-STAFF-1" {
			found = true
			if item.Availability != "in_use" {
				t.Fatalf("availability = %q, want in_use", item.Availability)
			}
		}
	}
	if !found {
		t.Fatal("the borrowed device is missing from the staff list entirely; it should be listed as in use")
	}
}

func TestAStaffMemberSeesOnlyTheirOwnLoans(t *testing.T) {
	env := newHTTPTestEnv(t)

	mine := env.StaffUser // the user behind env.StaffSessionToken
	other := env.SeedUser(t, "E-OTHER-1", "Other Person", "other@hospital.example")
	myDevice := env.SeedDevice(t, "AT-MINE-1", "My Laptop")
	theirDevice := env.SeedDevice(t, "AT-THEIRS-1", "Their Laptop")
	env.SeedOpenLoan(t, myDevice.ID, mine.ID)
	env.SeedOpenLoan(t, theirDevice.ID, other.ID)

	req := httptest.NewRequest(http.MethodGet, "/v1/staff/me/loans", nil)
	req.AddCookie(&http.Cookie{Name: "hdms_staff_session", Value: env.StaffSessionToken})
	rec := httptest.NewRecorder()
	env.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/staff/me/loans = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Items []struct {
			DeviceAssetTag string `json:"deviceAssetTag"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Items) != 1 {
		t.Fatalf("loans returned = %d, want exactly 1 — only the signed-in user's own", len(payload.Items))
	}
	if payload.Items[0].DeviceAssetTag != "AT-MINE-1" {
		t.Fatalf("loan for %q, want AT-MINE-1 — another user's loan leaked", payload.Items[0].DeviceAssetTag)
	}
}

func TestStaffMeCredentialReturnsActiveQR(t *testing.T) {
	env := newHTTPTestEnv(t)
	mine := env.StaffUser

	cardID := env.SeedActiveUserCard(t, mine.ID)

	req := httptest.NewRequest(http.MethodGet, "/v1/staff/me/credential", nil)
	req.AddCookie(&http.Cookie{Name: "hdms_staff_session", Value: env.StaffSessionToken})
	rec := httptest.NewRecorder()
	env.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/staff/me/credential = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Id    string `json:"id"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Id != cardID {
		t.Fatalf("credential id = %q, want %q", payload.Id, cardID)
	}
	if payload.Token == "" {
		t.Fatal("token should not be empty")
	}
}

func TestStaffMeCredentialReturns404WhenNoCredential(t *testing.T) {
	env := newHTTPTestEnv(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/staff/me/credential", nil)
	req.AddCookie(&http.Cookie{Name: "hdms_staff_session", Value: env.StaffSessionToken})
	rec := httptest.NewRecorder()
	env.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /v1/staff/me/credential with no card = %d, want 404: %s", rec.Code, rec.Body.String())
	}
}

func TestStaffDeviceDetailNeverNamesBorrower(t *testing.T) {
	env := newHTTPTestEnv(t)
	borrower := env.SeedUser(t, "E-BORROW-2", "Dr Borrower", "borrower2@hospital.example")
	device := env.SeedDevice(t, "AT-STAFF-2", "Defibrillator")
	due := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	_, err := env.Lending.OpenLoan(t.Context(), device.ID, borrower.ID, &due, lendingapi.OpenMeta{
		Actor:  "admin:test",
		Source: "manual",
	})
	if err != nil {
		t.Fatalf("OpenLoan: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/staff/devices/"+device.ID, nil)
	req.AddCookie(&http.Cookie{Name: "hdms_staff_session", Value: env.StaffSessionToken})
	rec := httptest.NewRecorder()
	env.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/staff/devices/{id} = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "Dr Borrower") || strings.Contains(body, borrower.ID) {
		t.Fatalf("the staff device detail disclosed the borrower: %s", body)
	}

	var payload struct {
		Id             string `json:"id"`
		AssetTag       string `json:"assetTag"`
		Availability   string `json:"availability"`
		ExpectedBackAt string `json:"expectedBackAt"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.AssetTag != "AT-STAFF-2" {
		t.Fatalf("assetTag = %q, want AT-STAFF-2", payload.AssetTag)
	}
	if payload.Availability != "in_use" {
		t.Fatalf("availability = %q, want in_use", payload.Availability)
	}
	if payload.ExpectedBackAt == "" {
		t.Fatal("expectedBackAt should not be empty for an in-use device")
	}
}
