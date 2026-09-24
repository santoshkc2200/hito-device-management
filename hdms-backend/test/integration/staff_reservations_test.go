//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/audit"
	"github.com/hito-hospital/hdms/internal/modules/lending"
	"github.com/hito-hospital/hdms/internal/modules/lending/lendingapi"
	"github.com/hito-hospital/hdms/internal/modules/reservations"
	"github.com/hito-hospital/hdms/internal/modules/reservations/reservationsapi"
	"github.com/hito-hospital/hdms/internal/platform/clock"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
)

func staffReservationRequest(t *testing.T, env *httpTestEnv, body string, csrf bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/staff/me/reservations", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "hdms_staff_session", Value: env.StaffSessionToken})
	if csrf {
		session, err := env.StaffAuth.ValidateSession(t.Context(), env.StaffSessionToken)
		if err != nil {
			t.Fatalf("validate staff session: %v", err)
		}
		req.Header.Set("X-CSRF-Token", session.CSRFToken)
	}
	rec := httptest.NewRecorder()
	env.Handler.ServeHTTP(rec, req)
	return rec
}

func TestStaffCanReserveOnlyForThemselves(t *testing.T) {
	env := newHTTPTestEnv(t)
	device := env.SeedDevice(t, "AT-SELF-BOOK", "Self-booked iPad")
	other := env.SeedUser(t, "E-OTHER-BOOK", "Other Person", "other.book@hospital.example")
	start := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second)
	body := fmt.Sprintf(`{"deviceId":%q,"userId":%q,"startAt":%q,"endAt":%q}`,
		device.ID, other.ID, start.Format(time.RFC3339), start.Add(time.Hour).Format(time.RFC3339))
	rec := staffReservationRequest(t, env, body, true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /v1/staff/me/reservations = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var payload gen.StaffReservation
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.DeviceAssetTag != "AT-SELF-BOOK" || payload.Status != gen.ReservationStatusActive {
		t.Fatalf("unexpected staff reservation: %+v", payload)
	}
	if strings.Contains(rec.Body.String(), other.ID) || strings.Contains(rec.Body.String(), "Other Person") {
		t.Fatal("staff reservation response disclosed another user")
	}
	var userID, actor, source string
	if err := env.Pool.QueryRow(t.Context(), `SELECT user_id, created_by, created_source FROM reservations WHERE id = $1`, payload.Id).Scan(&userID, &actor, &source); err != nil {
		t.Fatalf("read created reservation: %v", err)
	}
	if userID != env.StaffUser.ID || actor != "staff:"+env.StaffUser.ID || source != "staff" {
		t.Fatalf("reservation attribution = (%s, %s, %s)", userID, actor, source)
	}
}

func TestStaffReservationCreateAndCancelAreAuditedWithStaffActor(t *testing.T) {
	env := newHTTPTestEnv(t)
	device := env.SeedDevice(t, "AT-BOOK-AUDIT", "Audited iPad")
	start := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second)
	body := fmt.Sprintf(`{"deviceId":%q,"startAt":%q,"endAt":%q}`,
		device.ID, start.Format(time.RFC3339), start.Add(time.Hour).Format(time.RFC3339))
	rec := staffReservationRequest(t, env, body, true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /v1/staff/me/reservations = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var payload gen.StaffReservation
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if rec := staffCancelReservationRequest(t, env, payload.Id.String()); rec.Code != http.StatusOK {
		t.Fatalf("cancel = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	rows, err := env.Pool.Query(t.Context(),
		`SELECT action, actor FROM audit_events WHERE subject = $1 ORDER BY at, action`,
		"reservation:"+payload.Id.String())
	if err != nil {
		t.Fatalf("read audit events: %v", err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var action, actor string
		if err := rows.Scan(&action, &actor); err != nil {
			t.Fatalf("scan audit event: %v", err)
		}
		got = append(got, action+" by "+actor)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate audit events: %v", err)
	}
	staffActor := "staff:" + env.StaffUser.ID
	want := []string{"reservation.created by " + staffActor, "reservation.cancelled by " + staffActor}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("audit events = %v, want %v", got, want)
	}
}

func TestStaffReservationRequiresCSRF(t *testing.T) {
	env := newHTTPTestEnv(t)
	device := env.SeedDevice(t, "AT-BOOK-CSRF", "Protected iPad")
	start := time.Now().UTC().Add(time.Hour)
	body := fmt.Sprintf(`{"deviceId":%q,"startAt":%q,"endAt":%q}`,
		device.ID, start.Format(time.RFC3339), start.Add(time.Hour).Format(time.RFC3339))
	rec := staffReservationRequest(t, env, body, false)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST without CSRF = %d, want 403: %s", rec.Code, rec.Body.String())
	}
}

func TestStaffReservationHasBoundedWindow(t *testing.T) {
	env := newHTTPTestEnv(t)
	device := env.SeedDevice(t, "AT-BOOK-BOUNDS", "Bounded iPad")
	for _, tc := range []struct {
		name  string
		start time.Time
		end   time.Time
	}{
		{"too far ahead", time.Now().Add(91 * 24 * time.Hour), time.Now().Add(92 * 24 * time.Hour)},
		{"too long", time.Now().Add(2 * time.Hour), time.Now().Add(31 * 24 * time.Hour)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"deviceId":%q,"startAt":%q,"endAt":%q}`,
				device.ID, tc.start.Format(time.RFC3339), tc.end.Format(time.RFC3339))
			rec := staffReservationRequest(t, env, body, true)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("unbounded booking = %d, want 422: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestAdminBookingPolicyControlsStaffReservations(t *testing.T) {
	env := newHTTPTestEnv(t)
	admin := env.AdminSessionForRole(t, "admin")
	anonymous := httptest.NewRecorder()
	env.Handler.ServeHTTP(anonymous, httptest.NewRequest(http.MethodGet, "/v1/staff/booking-policy", nil))
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous booking policy read = %d, want 401", anonymous.Code)
	}
	getPolicy := func() map[string]int {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/v1/staff/booking-policy", nil)
		req.AddCookie(&http.Cookie{Name: "hdms_staff_session", Value: env.StaffSessionToken})
		rec := httptest.NewRecorder()
		env.Handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET staff booking policy = %d: %s", rec.Code, rec.Body.String())
		}
		var policy map[string]int
		if err := json.Unmarshal(rec.Body.Bytes(), &policy); err != nil {
			t.Fatalf("decode policy: %v", err)
		}
		return policy
	}
	if p := getPolicy(); p["advanceDays"] != 90 || p["maxDurationDays"] != 30 || p["returnBufferMinutes"] != 60 {
		t.Fatalf("booking defaults = %v", p)
	}
	bad := httptest.NewRequest(http.MethodPatch, "/v1/settings", strings.NewReader(`{"bookingPolicy":{"advanceDays":0,"maxDurationDays":1,"returnBufferMinutes":90}}`))
	bad.Header.Set("Content-Type", "application/json")
	bad.Header.Set("X-CSRF-Token", admin.CSRFToken)
	bad.AddCookie(&http.Cookie{Name: "hdms_session", Value: admin.Token})
	badRec := httptest.NewRecorder()
	env.Handler.ServeHTTP(badRec, bad)
	if badRec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid booking policy = %d, want 422: %s", badRec.Code, badRec.Body.String())
	}
	update := `{"bookingPolicy":{"advanceDays":3,"maxDurationDays":1,"returnBufferMinutes":90}}`
	req := httptest.NewRequest(http.MethodPatch, "/v1/settings", strings.NewReader(update))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", admin.CSRFToken)
	req.AddCookie(&http.Cookie{Name: "hdms_session", Value: admin.Token})
	rec := httptest.NewRecorder()
	env.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH booking policy = %d: %s", rec.Code, rec.Body.String())
	}
	if p := getPolicy(); p["advanceDays"] != 3 || p["maxDurationDays"] != 1 || p["returnBufferMinutes"] != 90 {
		t.Fatalf("updated booking policy = %v", p)
	}
	device := env.SeedDevice(t, "AT-BOOK-CUSTOM", "Custom policy iPad")
	book := func(start, end time.Time) *httptest.ResponseRecorder {
		body := fmt.Sprintf(`{"deviceId":%q,"startAt":%q,"endAt":%q}`,
			device.ID, start.Format(time.RFC3339), end.Format(time.RFC3339))
		return staffReservationRequest(t, env, body, true)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if got := book(now.Add(4*24*time.Hour), now.Add(4*24*time.Hour+time.Hour)); got.Code != http.StatusUnprocessableEntity {
		t.Fatalf("beyond custom horizon = %d: %s", got.Code, got.Body.String())
	}
	if got := book(now.Add(2*time.Hour), now.Add(27*time.Hour)); got.Code != http.StatusUnprocessableEntity {
		t.Fatalf("beyond custom duration = %d: %s", got.Code, got.Body.String())
	}
	if got := book(now.Add(2*time.Hour), now.Add(3*time.Hour)); got.Code != http.StatusCreated {
		t.Fatalf("within custom policy = %d: %s", got.Code, got.Body.String())
	}
	borrower := env.SeedUser(t, "E-CUSTOM-BOOK-BORROWER", "Borrower", "custom.book@hospital.example")
	loaned := env.SeedDevice(t, "AT-BOOK-CUSTOM-LOAN", "Loaned iPad")
	due := now.Add(2 * time.Hour)
	if _, err := env.Lending.OpenLoan(t.Context(), loaned.ID, borrower.ID, &due,
		lendingapi.OpenMeta{Actor: "admin:test", Source: "manual"}); err != nil {
		t.Fatalf("open loan: %v", err)
	}
	bookLoaned := func(start time.Time) *httptest.ResponseRecorder {
		body := fmt.Sprintf(`{"deviceId":%q,"startAt":%q,"endAt":%q}`,
			loaned.ID, start.Format(time.RFC3339), start.Add(time.Hour).Format(time.RFC3339))
		return staffReservationRequest(t, env, body, true)
	}
	if got := bookLoaned(due.Add(time.Hour)); got.Code != http.StatusConflict {
		t.Fatalf("before custom return buffer = %d: %s", got.Code, got.Body.String())
	}
	if got := bookLoaned(due.Add(90 * time.Minute)); got.Code != http.StatusCreated {
		t.Fatalf("after custom return buffer = %d: %s", got.Code, got.Body.String())
	}
}

func TestStaffReservationsExposeNextPage(t *testing.T) {
	env := newHTTPTestEnv(t)
	device := env.SeedDevice(t, "AT-BOOK-PAGES", "Paged iPad")
	start := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second)
	_, err := env.Pool.Exec(t.Context(), `INSERT INTO reservations (id, device_id, user_id, start_at, end_at, created_by, created_source)
		SELECT gen_random_uuid(), $1, $2, $3::timestamptz + n * interval '1 hour',
		$3::timestamptz + n * interval '1 hour' + interval '30 minutes', 'admin:test', 'admin'
		FROM generate_series(0, 100) AS n`, device.ID, env.StaffUser.ID, start)
	if err != nil {
		t.Fatalf("seed reservations: %v", err)
	}
	get := func(cursor string) gen.StaffReservationList {
		t.Helper()
		url := "/v1/staff/me/reservations"
		if cursor != "" {
			url += "?cursor=" + cursor
		}
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.AddCookie(&http.Cookie{Name: "hdms_staff_session", Value: env.StaffSessionToken})
		rec := httptest.NewRecorder()
		env.Handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("list page = %d: %s", rec.Code, rec.Body.String())
		}
		var page gen.StaffReservationList
		if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
			t.Fatalf("decode page: %v", err)
		}
		return page
	}
	first := get("")
	if len(first.Items) != 100 || first.NextCursor == nil {
		t.Fatalf("first page: items=%d cursor=%v", len(first.Items), first.NextCursor)
	}
	second := get(*first.NextCursor)
	if len(second.Items) != 1 || second.NextCursor != nil {
		t.Fatalf("second page: items=%d cursor=%v", len(second.Items), second.NextCursor)
	}
}

func TestStaffBookingWaitsForConcurrentDeviceStatusChange(t *testing.T) {
	env := newHTTPTestEnv(t)
	device := env.SeedDevice(t, "AT-BOOK-STATUS-RACE", "Changing iPad")
	tx, err := env.Pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin status change: %v", err)
	}
	defer tx.Rollback(t.Context())
	if _, err := tx.Exec(t.Context(), `UPDATE devices SET status = 'maintenance' WHERE id = $1`, device.ID); err != nil {
		t.Fatalf("change device status: %v", err)
	}
	start := time.Now().UTC().Add(2 * time.Hour)
	body := fmt.Sprintf(`{"deviceId":%q,"startAt":%q,"endAt":%q}`,
		device.ID, start.Format(time.RFC3339), start.Add(time.Hour).Format(time.RFC3339))
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- staffReservationRequest(t, env, body, true) }()
	select {
	case rec := <-done:
		t.Fatalf("booking completed before status change committed: %d", rec.Code)
	case <-time.After(50 * time.Millisecond):
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatalf("commit status change: %v", err)
	}
	select {
	case rec := <-done:
		if rec.Code != http.StatusConflict {
			t.Fatalf("booking after maintenance status = %d, want 409: %s", rec.Code, rec.Body.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("booking did not finish after status change committed")
	}
}

func TestStaffBookingWaitsForConcurrentNewLoan(t *testing.T) {
	env := newHTTPTestEnv(t)
	device := env.SeedDevice(t, "AT-BOOK-LOAN-RACE", "Borrowed iPad")
	start := time.Now().UTC().Add(2 * time.Hour)
	body := fmt.Sprintf(`{"deviceId":%q,"startAt":%q,"endAt":%q}`,
		device.ID, start.Format(time.RFC3339), start.Add(time.Hour).Format(time.RFC3339))
	done := make(chan *httptest.ResponseRecorder, 1)
	err := db.NewTxManager(env.Pool).Do(t.Context(), func(ctx context.Context) error {
		_, err := lending.New(env.Pool, audit.New(env.Pool), clock.System{}).OpenLoan(ctx, device.ID, env.StaffUser.ID, nil,
			lendingapi.OpenMeta{Actor: "admin:test", Source: "manual"})
		if err != nil {
			return err
		}
		go func() { done <- staffReservationRequest(t, env, body, true) }()
		select {
		case rec := <-done:
			// Return rather than t.Fatalf so the loan transaction rolls back
			// instead of holding its locks through test cleanup.
			return fmt.Errorf("booking completed before loan committed: %d", rec.Code)
		case <-time.After(50 * time.Millisecond):
		}
		return nil
	})
	if err != nil {
		t.Fatalf("open concurrent loan: %v", err)
	}
	select {
	case rec := <-done:
		if rec.Code != http.StatusConflict {
			t.Fatalf("booking after loan without due date = %d, want 409: %s", rec.Code, rec.Body.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("booking did not finish after loan committed")
	}
}

func staffCancelReservationRequest(t *testing.T, env *httpTestEnv, reservationID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/staff/me/reservations/"+reservationID+"/cancel", nil)
	req.AddCookie(&http.Cookie{Name: "hdms_staff_session", Value: env.StaffSessionToken})
	session, err := env.StaffAuth.ValidateSession(t.Context(), env.StaffSessionToken)
	if err != nil {
		t.Fatalf("validate staff session: %v", err)
	}
	req.Header.Set("X-CSRF-Token", session.CSRFToken)
	rec := httptest.NewRecorder()
	env.Handler.ServeHTTP(rec, req)
	return rec
}

func TestStaffCanCancelOnlyTheirOwnReservation(t *testing.T) {
	env := newHTTPTestEnv(t)
	auditSvc := audit.New(env.Pool)
	resSvc := reservations.New(env.Pool, auditSvc, clock.System{})
	other := env.SeedUser(t, "E-OTHER-CANCEL", "Other Person", "other.cancel@hospital.example")
	mineDevice := env.SeedDevice(t, "AT-CANCEL-MINE", "My iPad")
	otherDevice := env.SeedDevice(t, "AT-CANCEL-OTHER", "Other iPad")
	start := time.Now().UTC().Add(time.Hour)
	create := func(deviceID, userID string) reservationsapi.Reservation {
		t.Helper()
		res, err := resSvc.CreateReservation(t.Context(), reservationsapi.CreateParams{
			DeviceID: deviceID, UserID: userID, StartAt: start, EndAt: start.Add(time.Hour),
			CreatedBy: "admin:test", CreatedSource: "admin",
		})
		if err != nil {
			t.Fatalf("seed reservation: %v", err)
		}
		return res
	}
	mine := create(mineDevice.ID, env.StaffUser.ID)
	theirs := create(otherDevice.ID, other.ID)

	foreign := staffCancelReservationRequest(t, env, theirs.ID)
	if foreign.Code != http.StatusNotFound {
		t.Fatalf("cancel another staff member's reservation = %d, want 404: %s", foreign.Code, foreign.Body.String())
	}
	result := staffCancelReservationRequest(t, env, mine.ID)
	if result.Code != http.StatusOK {
		t.Fatalf("cancel own reservation = %d, want 200: %s", result.Code, result.Body.String())
	}
	got, err := resSvc.GetReservation(t.Context(), mine.ID)
	if err != nil {
		t.Fatalf("read cancelled reservation: %v", err)
	}
	if got.Status != reservationsapi.StatusCancelled || got.CancelledBy == nil || *got.CancelledBy != "staff:"+env.StaffUser.ID {
		t.Fatalf("own reservation not cancelled by staff: %+v", got)
	}
	otherAfter, err := resSvc.GetReservation(t.Context(), theirs.ID)
	if err != nil {
		t.Fatalf("read other reservation: %v", err)
	}
	if otherAfter.Status != reservationsapi.StatusActive {
		t.Fatalf("other reservation changed: %+v", otherAfter)
	}
}

func TestStaffBookingRejectsPastUnavailableAndOverlappingWindows(t *testing.T) {
	env := newHTTPTestEnv(t)
	now := time.Now().UTC().Truncate(time.Second)
	requestBody := func(deviceID string, start, end time.Time) string {
		return fmt.Sprintf(`{"deviceId":%q,"startAt":%q,"endAt":%q}`,
			deviceID, start.Format(time.RFC3339), end.Format(time.RFC3339))
	}
	device := env.SeedDevice(t, "AT-BOOK-VALIDATION", "Validation iPad")
	past := staffReservationRequest(t, env, requestBody(device.ID, now.Add(-time.Hour), now.Add(time.Hour)), true)
	if past.Code != http.StatusUnprocessableEntity {
		t.Fatalf("past booking = %d, want 422: %s", past.Code, past.Body.String())
	}

	unavailable := env.SeedDevice(t, "AT-BOOK-MAINT", "Maintenance iPad")
	if _, err := env.Catalog.SetStatus(t.Context(), unavailable.ID, "maintenance", "test", "admin:test"); err != nil {
		t.Fatalf("set maintenance: %v", err)
	}
	blocked := staffReservationRequest(t, env, requestBody(unavailable.ID, now.Add(time.Hour), now.Add(2*time.Hour)), true)
	if blocked.Code != http.StatusConflict {
		t.Fatalf("maintenance booking = %d, want 409: %s", blocked.Code, blocked.Body.String())
	}

	resSvc := reservations.New(env.Pool, audit.New(env.Pool), clock.System{})
	_, err := resSvc.CreateReservation(t.Context(), reservationsapi.CreateParams{
		DeviceID: device.ID, UserID: env.StaffUser.ID,
		StartAt: now.Add(2 * time.Hour), EndAt: now.Add(4 * time.Hour),
		CreatedBy: "admin:test", CreatedSource: "admin",
	})
	if err != nil {
		t.Fatalf("seed overlap: %v", err)
	}
	overlap := staffReservationRequest(t, env, requestBody(device.ID, now.Add(3*time.Hour), now.Add(5*time.Hour)), true)
	if overlap.Code != http.StatusConflict || !strings.Contains(overlap.Body.String(), "reservation-conflict") {
		t.Fatalf("overlapping booking = %d, want reservation-conflict: %s", overlap.Code, overlap.Body.String())
	}
}

func TestStaffBookingKeepsReturnGapFromOtherReservations(t *testing.T) {
	env := newHTTPTestEnv(t)
	device := env.SeedDevice(t, "AT-BOOK-GAP", "Back-to-back iPad")
	now := time.Now().UTC().Truncate(time.Second)
	// Seeded by an admin, which does not apply the staff booking policy.
	seededStart, seededEnd := now.Add(3*time.Hour), now.Add(5*time.Hour)
	resSvc := reservations.New(env.Pool, audit.New(env.Pool), clock.System{})
	if _, err := resSvc.CreateReservation(t.Context(), reservationsapi.CreateParams{
		DeviceID: device.ID, UserID: env.StaffUser.ID,
		StartAt: seededStart, EndAt: seededEnd,
		CreatedBy: "admin:test", CreatedSource: "admin",
	}); err != nil {
		t.Fatalf("seed neighbour: %v", err)
	}
	book := func(start, end time.Time) *httptest.ResponseRecorder {
		body := fmt.Sprintf(`{"deviceId":%q,"startAt":%q,"endAt":%q}`,
			device.ID, start.Format(time.RFC3339), end.Format(time.RFC3339))
		return staffReservationRequest(t, env, body, true)
	}

	// The default policy leaves 60 minutes between reservations.
	for name, window := range map[string][2]time.Time{
		"starts too soon after": {seededEnd.Add(30 * time.Minute), seededEnd.Add(2 * time.Hour)},
		"ends too close before": {now.Add(time.Hour), seededStart.Add(-30 * time.Minute)},
	} {
		rec := book(window[0], window[1])
		if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "reservation-too-close") {
			t.Fatalf("booking that %s = %d, want reservation-too-close: %s", name, rec.Code, rec.Body.String())
		}
	}
	if overlap := book(seededStart.Add(-30*time.Minute), seededStart.Add(30*time.Minute)); !strings.Contains(overlap.Body.String(), "reservation-conflict") {
		t.Fatalf("overlapping booking = %d, want reservation-conflict: %s", overlap.Code, overlap.Body.String())
	}

	if after := book(seededEnd.Add(time.Hour), seededEnd.Add(2*time.Hour)); after.Code != http.StatusCreated {
		t.Fatalf("booking exactly one gap after = %d, want 201: %s", after.Code, after.Body.String())
	}
	if before := book(now.Add(time.Hour), seededStart.Add(-time.Hour)); before.Code != http.StatusCreated {
		t.Fatalf("booking ending exactly one gap before = %d, want 201: %s", before.Code, before.Body.String())
	}
	var count int
	if err := env.Pool.QueryRow(t.Context(), `SELECT count(*) FROM reservations WHERE device_id = $1`, device.ID).Scan(&count); err != nil {
		t.Fatalf("count reservations: %v", err)
	}
	if count != 3 {
		t.Fatalf("reservations for device = %d, want 3 (rejected bookings must not persist)", count)
	}
}

func TestStaffBookingOnLoanStartsAfterExpectedReturnGap(t *testing.T) {
	env := newHTTPTestEnv(t)
	borrower := env.SeedUser(t, "E-BOOK-BORROWER", "Borrower", "book.borrower@hospital.example")
	device := env.SeedDevice(t, "AT-BOOK-ON-LOAN", "Loaned iPad")
	due := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second)
	if _, err := env.Lending.OpenLoan(t.Context(), device.ID, borrower.ID, &due, lendingapi.OpenMeta{
		Actor: "admin:test", Source: "manual",
	}); err != nil {
		t.Fatalf("open loan: %v", err)
	}
	book := func(start time.Time) *httptest.ResponseRecorder {
		body := fmt.Sprintf(`{"deviceId":%q,"startAt":%q,"endAt":%q}`,
			device.ID, start.Format(time.RFC3339), start.Add(time.Hour).Format(time.RFC3339))
		return staffReservationRequest(t, env, body, true)
	}
	tooEarly := book(due.Add(30 * time.Minute))
	if tooEarly.Code != http.StatusConflict || !strings.Contains(tooEarly.Body.String(), "device-in-use") {
		t.Fatalf("booking before gap = %d, want device-in-use: %s", tooEarly.Code, tooEarly.Body.String())
	}
	afterGap := book(due.Add(time.Hour))
	if afterGap.Code != http.StatusCreated {
		t.Fatalf("booking after gap = %d, want 201: %s", afterGap.Code, afterGap.Body.String())
	}
}

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
