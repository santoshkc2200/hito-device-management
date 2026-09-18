package jobs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/audit"
	"github.com/hito-hospital/hdms/internal/modules/notification/notificationapi"
	"github.com/hito-hospital/hdms/internal/modules/reservations"
	"github.com/hito-hospital/hdms/internal/modules/reservations/reservationsapi"
	"github.com/hito-hospital/hdms/internal/platform/clock"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/observability"
	"github.com/hito-hospital/hdms/internal/platform/settings"
)

// ReservationExpiryReport summarizes the execution of a reservation expiry run (6.4e).
type ReservationExpiryReport struct {
	GraceMinutes   int `json:"grace_minutes"`
	ExpiredCount   int `json:"expired_count"`
	NotifsEnqueued int `json:"notifs_enqueued"`
	SkippedDedupe  int `json:"skipped_dedupe"`
}

// RunReservationExpiry marks uncollected reservations past start_at + grace_period as expired,
// notifies reservers via email, updates observability metrics, records job_runs, and exports
// the last-success metric for node_exporter textfiles.
func RunReservationExpiry(
	ctx context.Context,
	pool *db.Pool,
	resSvc reservationsapi.Service,
	settingsSvc *settings.Service,
	notifSvc notificationapi.Service,
	now time.Time,
	metricsDir string,
) (ReservationExpiryReport, error) {
	started := now.UTC()
	var report ReservationExpiryReport

	// Initialize default services if not injected and database pool is available
	if resSvc == nil && pool != nil {
		resSvc = reservations.New(pool, audit.New(pool), clock.System{})
	}
	if settingsSvc == nil && pool != nil {
		settingsSvc = settings.New(pool, nil)
	}

	graceMinutes := 15
	if settingsSvc != nil {
		st, err := settingsSvc.GetSettings(ctx)
		if err == nil && st.Policy.ReservationExpiryGraceMinutes > 0 {
			graceMinutes = st.Policy.ReservationExpiryGraceMinutes
		}
	}
	grace := time.Duration(graceMinutes) * time.Minute
	report.GraceMinutes = graceMinutes

	expired, err := resSvc.ExpireNoShows(ctx, grace, started)
	if err != nil {
		finished := time.Now().UTC()
		if pool != nil {
			_ = recordJobRun(ctx, pool.Pool, "reservation-expiry", started, finished, "failure",
				map[string]any{"stage": "expire_no_shows", "error": err.Error()})
		}
		return ReservationExpiryReport{}, fmt.Errorf("jobs: expire no-show reservations: %w", err)
	}

	report.ExpiredCount = len(expired)
	observability.AddReservationsExpired(len(expired))

	for _, r := range expired {
		if r.UserEmail == "" || notifSvc == nil {
			continue
		}
		dedupeKey := fmt.Sprintf("reservation-expiry:%s", r.ID)
		params := notificationapi.EnqueueParams{
			Recipient: r.UserEmail,
			Channel:   notificationapi.ChannelEmail,
			Template:  notificationapi.TemplateReservationExpired,
			DedupeKey: dedupeKey,
			UserID:    r.UserID,
			Payload: map[string]any{
				"reservationId":  r.ID,
				"deviceId":       r.DeviceID,
				"deviceName":     r.DeviceName,
				"deviceAssetTag": r.DeviceAssetTag,
				"userId":         r.UserID,
				"userName":       r.UserName,
				"startAt":        r.StartAt.Format("2006-01-02 15:04"),
				"endAt":          r.EndAt.Format("2006-01-02 15:04"),
				"graceMinutes":   graceMinutes,
			},
		}

		if _, err := notifSvc.Enqueue(ctx, params); err != nil {
			if errors.Is(err, notificationapi.ErrDuplicateDelivery) {
				report.SkippedDedupe++
			}
		} else {
			report.NotifsEnqueued++
		}
	}

	if notifSvc != nil && report.NotifsEnqueued > 0 {
		_, _ = notifSvc.ProcessPendingDeliveries(ctx, report.NotifsEnqueued)
	}

	finished := time.Now().UTC()
	detail := map[string]any{
		"grace_minutes":   report.GraceMinutes,
		"expired_count":   report.ExpiredCount,
		"notifs_enqueued": report.NotifsEnqueued,
		"skipped_dedupe":  report.SkippedDedupe,
	}

	if pool != nil {
		if err := recordJobRun(ctx, pool.Pool, "reservation-expiry", started, finished, "success", detail); err != nil {
			return report, err
		}
	}
	if err := WriteLastSuccess(metricsDir, "reservation-expiry", finished); err != nil {
		return report, err
	}

	return report, nil
}
