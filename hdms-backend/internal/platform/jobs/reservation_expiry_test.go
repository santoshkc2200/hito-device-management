package jobs_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/notification/notificationapi"
	"github.com/hito-hospital/hdms/internal/modules/reservations/reservationsapi"
	"github.com/hito-hospital/hdms/internal/platform/jobs"
	"github.com/hito-hospital/hdms/internal/platform/observability"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

type mockReservationsService struct {
	reservationsapi.Service
	expireFn func(ctx context.Context, grace time.Duration, now time.Time) ([]reservationsapi.Reservation, error)
}

func (m *mockReservationsService) ExpireNoShows(ctx context.Context, grace time.Duration, now time.Time) ([]reservationsapi.Reservation, error) {
	if m.expireFn != nil {
		return m.expireFn(ctx, grace, now)
	}
	return nil, nil
}

type mockNotificationService struct {
	notificationapi.Service
	enqueued []notificationapi.EnqueueParams
	failWith error
}

func (m *mockNotificationService) Enqueue(ctx context.Context, params notificationapi.EnqueueParams) (notificationapi.Delivery, error) {
	if m.failWith != nil {
		return notificationapi.Delivery{}, m.failWith
	}
	m.enqueued = append(m.enqueued, params)
	return notificationapi.Delivery{ID: "del-1"}, nil
}

func (m *mockNotificationService) ProcessPendingDeliveries(ctx context.Context, limit int) (int, error) {
	return len(m.enqueued), nil
}

func TestRunReservationExpirySuccessful(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 18, 15, 0, 0, 0, time.UTC)
	startAt := now.Add(-30 * time.Minute)
	endAt := startAt.Add(2 * time.Hour)

	mockRes := &mockReservationsService{
		expireFn: func(ctx context.Context, grace time.Duration, at time.Time) ([]reservationsapi.Reservation, error) {
			if grace != 15*time.Minute {
				t.Fatalf("expected default grace 15m, got %v", grace)
			}
			return []reservationsapi.Reservation{
				{
					ID:             "res-1",
					DeviceID:       "dev-1",
					DeviceName:     "Ultrasound Pro",
					DeviceAssetTag: "US-001",
					UserID:         "usr-1",
					UserName:       "Dr. Sato",
					UserEmail:      "sato@example.com",
					Status:         reservationsapi.StatusExpired,
					StartAt:        startAt,
					EndAt:          endAt,
				},
				{
					ID:             "res-2",
					DeviceID:       "dev-2",
					DeviceName:     "ECG Monitor",
					DeviceAssetTag: "ECG-002",
					UserID:         "usr-2",
					UserName:       "Nurse Suzuki",
					UserEmail:      "", // No email
					Status:         reservationsapi.StatusExpired,
					StartAt:        startAt,
					EndAt:          endAt,
				},
			}, nil
		},
	}

	mockNotif := &mockNotificationService{}

	metricBefore := testutil.ToFloat64(observability.ReservationsExpiredTotal)
	report, err := jobs.RunReservationExpiry(ctx, nil, mockRes, nil, mockNotif, now, "")
	if err != nil {
		t.Fatalf("RunReservationExpiry failed: %v", err)
	}

	if report.ExpiredCount != 2 {
		t.Fatalf("report.ExpiredCount = %d, want 2", report.ExpiredCount)
	}
	if report.NotifsEnqueued != 1 {
		t.Fatalf("report.NotifsEnqueued = %d, want 1", report.NotifsEnqueued)
	}
	if report.GraceMinutes != 15 {
		t.Fatalf("report.GraceMinutes = %d, want 15", report.GraceMinutes)
	}

	metricAfter := testutil.ToFloat64(observability.ReservationsExpiredTotal)
	if metricAfter-metricBefore != 2 {
		t.Fatalf("metric delta = %v, want 2", metricAfter-metricBefore)
	}

	if len(mockNotif.enqueued) != 1 {
		t.Fatalf("enqueued = %d, want 1", len(mockNotif.enqueued))
	}
	p := mockNotif.enqueued[0]
	if p.Recipient != "sato@example.com" {
		t.Fatalf("recipient = %s, want sato@example.com", p.Recipient)
	}
	if p.DedupeKey != "reservation-expiry:res-1" {
		t.Fatalf("dedupeKey = %s, want reservation-expiry:res-1", p.DedupeKey)
	}
	if p.Template != notificationapi.TemplateReservationExpired {
		t.Fatalf("template = %s, want %s", p.Template, notificationapi.TemplateReservationExpired)
	}
}

func TestRunReservationExpiryDeduplication(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 18, 15, 0, 0, 0, time.UTC)

	mockRes := &mockReservationsService{
		expireFn: func(ctx context.Context, grace time.Duration, at time.Time) ([]reservationsapi.Reservation, error) {
			return []reservationsapi.Reservation{
				{
					ID:        "res-1",
					UserEmail: "sato@example.com",
				},
			}, nil
		},
	}

	mockNotif := &mockNotificationService{
		failWith: notificationapi.ErrDuplicateDelivery,
	}

	report, err := jobs.RunReservationExpiry(ctx, nil, mockRes, nil, mockNotif, now, "")
	if err != nil {
		t.Fatalf("RunReservationExpiry failed: %v", err)
	}

	if report.ExpiredCount != 1 {
		t.Fatalf("report.ExpiredCount = %d, want 1", report.ExpiredCount)
	}
	if report.NotifsEnqueued != 0 {
		t.Fatalf("report.NotifsEnqueued = %d, want 0", report.NotifsEnqueued)
	}
	if report.SkippedDedupe != 1 {
		t.Fatalf("report.SkippedDedupe = %d, want 1", report.SkippedDedupe)
	}
}

func TestRunReservationExpiryServiceError(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 18, 15, 0, 0, 0, time.UTC)

	mockRes := &mockReservationsService{
		expireFn: func(ctx context.Context, grace time.Duration, at time.Time) ([]reservationsapi.Reservation, error) {
			return nil, errors.New("database connection failed")
		},
	}

	_, err := jobs.RunReservationExpiry(ctx, nil, mockRes, nil, nil, now, "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestRunReservationExpiryWritesMetricFile(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 18, 15, 0, 0, 0, time.UTC)
	dir := t.TempDir()

	mockRes := &mockReservationsService{
		expireFn: func(ctx context.Context, grace time.Duration, at time.Time) ([]reservationsapi.Reservation, error) {
			return []reservationsapi.Reservation{}, nil
		},
	}

	report, err := jobs.RunReservationExpiry(ctx, nil, mockRes, nil, nil, now, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.ExpiredCount != 0 {
		t.Fatalf("expired = %d, want 0", report.ExpiredCount)
	}

	promPath := filepath.Join(dir, "hdms_job_reservation-expiry.prom")
	data, err := os.ReadFile(promPath)
	if err != nil {
		t.Fatalf("failed to read prom file: %v", err)
	}
	if !strings.Contains(string(data), `hdms_job_last_success{job="reservation-expiry"}`) {
		t.Fatalf("prom file missing expected metric line: %s", string(data))
	}
	if !strings.Contains(string(data), "# TYPE hdms_job_last_success gauge") {
		t.Fatalf("prom file missing type header: %s", string(data))
	}
}
