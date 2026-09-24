//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/audit"
	"github.com/hito-hospital/hdms/internal/modules/reservations"
	"github.com/hito-hospital/hdms/internal/modules/reservations/reservationsapi"
	"github.com/hito-hospital/hdms/internal/platform/clock"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
)

func TestStaffMeReservationsSeesOnlyOwnActive(t *testing.T) {
	env := newHTTPTestEnv(t)
	auditSvc := audit.New(env.Pool)
	resSvc := reservations.New(env.Pool, auditSvc, clock.System{})

	mine := env.StaffUser
	other := env.SeedUser(t, "E-OTHER-RES-1", "Other Person", "other.res@hospital.example")
	myDevice := env.SeedDevice(t, "AT-RES-MINE", "My Reserved iPad")
	theirDevice := env.SeedDevice(t, "AT-RES-THEIRS", "Their Reserved iPad")

	now := time.Now().UTC().Truncate(time.Second)
	_, err := resSvc.CreateReservation(t.Context(), reservationsapi.CreateParams{
		DeviceID:      myDevice.ID,
		UserID:        mine.ID,
		StartAt:       now.Add(1 * time.Hour),
		EndAt:         now.Add(3 * time.Hour),
		CreatedBy:     "admin:test",
		CreatedSource: "admin",
	})
	if err != nil {
		t.Fatalf("create reservation for mine: %v", err)
	}

	_, err = resSvc.CreateReservation(t.Context(), reservationsapi.CreateParams{
		DeviceID:      theirDevice.ID,
		UserID:        other.ID,
		StartAt:       now.Add(1 * time.Hour),
		EndAt:         now.Add(3 * time.Hour),
		CreatedBy:     "admin:test",
		CreatedSource: "admin",
	})
	if err != nil {
		t.Fatalf("create reservation for other: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/staff/me/reservations", nil)
	req.AddCookie(&http.Cookie{Name: "hdms_staff_session", Value: env.StaffSessionToken})
	rec := httptest.NewRecorder()
	env.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/staff/me/reservations = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var payload gen.StaffReservationList
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Items) != 1 {
		t.Fatalf("reservations returned = %d, want exactly 1", len(payload.Items))
	}
	if payload.Items[0].DeviceAssetTag != "AT-RES-MINE" {
		t.Fatalf("got device %q, want AT-RES-MINE", payload.Items[0].DeviceAssetTag)
	}
	if payload.Items[0].DeviceName != "My Reserved iPad" {
		t.Fatalf("got name %q, want 'My Reserved iPad'", payload.Items[0].DeviceName)
	}
	if payload.Items[0].Status != gen.ReservationStatusActive {
		t.Fatalf("got status %q, want %q", payload.Items[0].Status, gen.ReservationStatusActive)
	}
}

func TestStaffMeReservationsExcludesCancelledAndExpired(t *testing.T) {
	env := newHTTPTestEnv(t)
	auditSvc := audit.New(env.Pool)
	resSvc := reservations.New(env.Pool, auditSvc, clock.System{})

	mine := env.StaffUser
	devActive := env.SeedDevice(t, "AT-RES-ACT", "Active Res iPad")
	devCancelled := env.SeedDevice(t, "AT-RES-CANC", "Cancelled Res iPad")
	devExpired := env.SeedDevice(t, "AT-RES-EXP", "Expired Res iPad")

	now := time.Now().UTC().Truncate(time.Second)
	// Active reservation
	_, err := resSvc.CreateReservation(t.Context(), reservationsapi.CreateParams{
		DeviceID:      devActive.ID,
		UserID:        mine.ID,
		StartAt:       now.Add(1 * time.Hour),
		EndAt:         now.Add(3 * time.Hour),
		CreatedBy:     "admin:test",
		CreatedSource: "admin",
	})
	if err != nil {
		t.Fatalf("create active reservation: %v", err)
	}

	// Cancelled reservation
	resCanc, err := resSvc.CreateReservation(t.Context(), reservationsapi.CreateParams{
		DeviceID:      devCancelled.ID,
		UserID:        mine.ID,
		StartAt:       now.Add(4 * time.Hour),
		EndAt:         now.Add(6 * time.Hour),
		CreatedBy:     "admin:test",
		CreatedSource: "admin",
	})
	if err != nil {
		t.Fatalf("create reservation to cancel: %v", err)
	}
	_, err = resSvc.CancelReservation(t.Context(), reservationsapi.CancelParams{
		ID:     resCanc.ID,
		Reason: "test cancellation",
		Actor:  "admin:test",
	})
	if err != nil {
		t.Fatalf("cancel reservation: %v", err)
	}

	// Expired reservation
	resExp, err := resSvc.CreateReservation(t.Context(), reservationsapi.CreateParams{
		DeviceID:      devExpired.ID,
		UserID:        mine.ID,
		StartAt:       now.Add(-2 * time.Hour),
		EndAt:         now.Add(-1 * time.Hour),
		CreatedBy:     "admin:test",
		CreatedSource: "admin",
	})
	if err != nil {
		t.Fatalf("create reservation to expire: %v", err)
	}
	if _, err := resSvc.ExpireNoShows(t.Context(), 15*time.Minute, now); err != nil {
		t.Fatalf("expire no-shows: %v", err)
	}
	expCheck, err := resSvc.GetReservation(t.Context(), resExp.ID)
	if err != nil {
		t.Fatalf("get reservation after expiry: %v", err)
	}
	if expCheck.Status != reservationsapi.StatusExpired {
		t.Fatalf("reservation status after ExpireNoShows = %q, want %q", expCheck.Status, reservationsapi.StatusExpired)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/staff/me/reservations", nil)
	req.AddCookie(&http.Cookie{Name: "hdms_staff_session", Value: env.StaffSessionToken})
	rec := httptest.NewRecorder()
	env.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/staff/me/reservations = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var payload gen.StaffReservationList
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Items) != 1 {
		t.Fatalf("reservations returned = %d, want 1 (only active)", len(payload.Items))
	}
	if payload.Items[0].DeviceAssetTag != "AT-RES-ACT" {
		t.Fatalf("got device %q, want AT-RES-ACT", payload.Items[0].DeviceAssetTag)
	}
}

func TestStaffMeReservationsEmptyWhenNoneExist(t *testing.T) {
	env := newHTTPTestEnv(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/staff/me/reservations", nil)
	req.AddCookie(&http.Cookie{Name: "hdms_staff_session", Value: env.StaffSessionToken})
	rec := httptest.NewRecorder()
	env.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/staff/me/reservations = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var payload gen.StaffReservationList
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Items) != 0 {
		t.Fatalf("expected 0 reservations, got %d", len(payload.Items))
	}
}
