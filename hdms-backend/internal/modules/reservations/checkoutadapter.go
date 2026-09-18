package reservations

import (
	"context"
	"fmt"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/checkout"
	"github.com/hito-hospital/hdms/internal/platform/settings"
)

// CheckoutAdapter satisfies checkout.ReservationLookup (Phase 6.4c),
// keeping the pre-window policy on this side of the boundary where it
// belongs. checkout asks only "is this device claimed right now"; how long
// before a window a device stops being walk-up borrowable is a
// reservations question, tunable by the counter through
// settings.reservation_pre_window_minutes.
type CheckoutAdapter struct {
	svc      *Service
	settings interface {
		GetSettings(ctx context.Context) (settings.Settings, error)
	}
}

// NewCheckoutAdapter wires the reservations service into checkout.
func NewCheckoutAdapter(svc *Service, s interface {
	GetSettings(ctx context.Context) (settings.Settings, error)
}) *CheckoutAdapter {
	return &CheckoutAdapter{svc: svc, settings: s}
}

// DefaultPreWindow is used when settings cannot be read. A scan must never
// fail because a policy knob was unreadable, and 30 minutes is the
// documented default the column itself carries.
const DefaultPreWindow = 30 * time.Minute

func (a *CheckoutAdapter) preWindow(ctx context.Context) time.Duration {
	if a.settings == nil {
		return DefaultPreWindow
	}
	st, err := a.settings.GetSettings(ctx)
	if err != nil || st.Policy.ReservationPreWindowMinutes <= 0 {
		return DefaultPreWindow
	}
	return time.Duration(st.Policy.ReservationPreWindowMinutes) * time.Minute
}

// InForceFor reports the reservation claiming deviceID at `at`, if any.
// "No reservation" is ok=false with a nil error — the common case, not a
// failure — so an unreserved device classifies exactly as it always did.
func (a *CheckoutAdapter) InForceFor(ctx context.Context, deviceID string, at time.Time) (checkout.ReservationInForce, bool, error) {
	res, _, err := a.svc.ActiveOrUpcomingForDevice(ctx, deviceID, at, a.preWindow(ctx))
	if err != nil {
		return checkout.ReservationInForce{}, false, fmt.Errorf("reservations: in force for device: %w", err)
	}
	if res == nil {
		return checkout.ReservationInForce{}, false, nil
	}
	return checkout.ReservationInForce{
		ID:        res.ID,
		ForUserID: res.UserID,
		StartAt:   res.StartAt,
	}, true, nil
}

// MarkCollected closes the reservation out against the loan just opened
// from it.
func (a *CheckoutAdapter) MarkCollected(ctx context.Context, reservationID, loanID string) error {
	if _, err := a.svc.FulfillReservation(ctx, reservationID, loanID); err != nil {
		return fmt.Errorf("reservations: mark collected: %w", err)
	}
	return nil
}
