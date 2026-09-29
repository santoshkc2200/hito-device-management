package reservations

import (
	"context"
	"fmt"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/checkout"
	"github.com/hito-hospital/hdms/internal/platform/settings"
)

// CheckoutAdapter satisfies checkout.ReservationLookup (Phase 6.4c),
// keeping the pre-window policy and return window on this side of the
// boundary where they belong. checkout asks only "is this device claimed
// right now" and "how long may this loan run"; how long before a window a
// device stops being walk-up borrowable is a reservations question, tunable
// by the counter through settings.reservation_pre_window_minutes.
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

// Defaults used when settings cannot be read. A scan must never fail
// because a policy knob was unreadable; these are the documented defaults
// the settings columns themselves carry.
const (
	DefaultPreWindow    = 30 * time.Minute
	DefaultReturnBuffer = 60 * time.Minute
	DefaultMaxLoan      = 30 * 24 * time.Hour
)

// MinUsableLoan is the shortest walk-up loan worth opening before the
// device's next reservation. When the booking gap leaves less than this,
// the reservation is treated as already in force, so the kiosk refuses the
// walk-up (or lets the reserver collect early) instead of opening a loan
// that must come back within minutes.
const MinUsableLoan = 30 * time.Minute

// policy is the slice of settings the checkout boundary needs.
type policy struct {
	preWindow time.Duration
	buffer    time.Duration
	maxLoan   time.Duration
}

// lead is how long before a reservation's start the device stops being
// walk-up borrowable.
func (p policy) lead() time.Duration {
	return max(p.preWindow, p.buffer+MinUsableLoan)
}

// latestReturn is the latest expected return for a loan opened at from,
// given the start of the next reservation that still needs the device.
func latestReturn(from time.Time, next *time.Time, p policy) time.Time {
	latest := from.Add(p.maxLoan)
	if next != nil {
		if bound := next.Add(-p.buffer); bound.Before(latest) {
			latest = bound
		}
	}
	return latest
}

func (a *CheckoutAdapter) policy(ctx context.Context) policy {
	p := policy{preWindow: DefaultPreWindow, buffer: DefaultReturnBuffer, maxLoan: DefaultMaxLoan}
	if a.settings == nil {
		return p
	}
	st, err := a.settings.GetSettings(ctx)
	if err != nil {
		return p
	}
	if st.Policy.ReservationPreWindowMinutes > 0 {
		p.preWindow = time.Duration(st.Policy.ReservationPreWindowMinutes) * time.Minute
	}
	if st.BookingPolicy.ReturnBufferMinutes >= 0 {
		p.buffer = time.Duration(st.BookingPolicy.ReturnBufferMinutes) * time.Minute
	}
	if st.BookingPolicy.MaxDurationDays > 0 {
		p.maxLoan = time.Duration(st.BookingPolicy.MaxDurationDays) * 24 * time.Hour
	}
	return p
}

// InForceFor reports the reservation claiming deviceID at `at`, if any.
// "No reservation" is ok=false with a nil error — the common case, not a
// failure — so an unreserved device classifies exactly as it always did.
// A reservation too close to leave MinUsableLoan after the booking gap is in force too.
func (a *CheckoutAdapter) InForceFor(ctx context.Context, deviceID string, at time.Time) (checkout.ReservationInForce, bool, error) {
	res, _, err := a.svc.ActiveOrUpcomingForDevice(ctx, deviceID, at, a.policy(ctx).lead())
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

// ReturnWindowFor reports how long a loan of deviceID opened at from may
// run (checkout.ReservationLookup).
func (a *CheckoutAdapter) ReturnWindowFor(ctx context.Context, deviceID string, from time.Time, collectingID string) (checkout.ReturnWindow, error) {
	var w checkout.ReturnWindow
	if collectingID != "" {
		res, err := a.svc.GetReservation(ctx, collectingID)
		if err != nil {
			return checkout.ReturnWindow{}, fmt.Errorf("reservations: reservation being collected: %w", err)
		}
		w.CollectedEndAt = res.EndAt
	}
	next, err := a.svc.NextActiveStartForDevice(ctx, deviceID, from, collectingID)
	if err != nil {
		return checkout.ReturnWindow{}, fmt.Errorf("reservations: return window: %w", err)
	}
	w.Latest = latestReturn(from, next, a.policy(ctx))
	return w, nil
}

// MarkCollected closes the reservation out against the loan just opened
// from it.
func (a *CheckoutAdapter) MarkCollected(ctx context.Context, reservationID, loanID string) error {
	if _, err := a.svc.FulfillReservation(ctx, reservationID, loanID); err != nil {
		return fmt.Errorf("reservations: mark collected: %w", err)
	}
	return nil
}
