package checkout

import (
	"context"
	"fmt"
	"time"
)

// DefaultLoanWithoutPeriod is the expected return offered when the device's
// category sets no loan period. Every kiosk loan carries a due date, so the
// borrower always has something to confirm or change, and staff booking can
// see when the device comes back.
const DefaultLoanWithoutPeriod = 24 * time.Hour

// chooseDueAt picks a new loan's expected return. The first of these wins:
// the end of the reservation being collected, the borrower's preferred
// return (their last choice in this session, if still in the future), the
// category's default period, or 24 hours. The result is then clamped to the
// return window's Latest (zero means unbounded) and never falls before now.
func chooseDueAt(now time.Time, w ReturnWindow, preferred, categoryDefault *time.Time) time.Time {
	var due time.Time
	switch {
	case !w.CollectedEndAt.IsZero():
		due = w.CollectedEndAt
	case preferred != nil && preferred.After(now):
		due = *preferred
	case categoryDefault != nil:
		due = *categoryDefault
	default:
		due = now.Add(DefaultLoanWithoutPeriod)
	}
	if !w.Latest.IsZero() && due.After(w.Latest) {
		due = w.Latest
	}
	if due.Before(now) {
		due = now
	}
	return due
}

// returnWindow asks the reservations module how long a loan of deviceID
// opened at from may run. A nil Reservations dep (deployments and tests
// predating 6.4) means no bound.
func (s *Service) returnWindow(ctx context.Context, deviceID, collectingID string, from time.Time) (ReturnWindow, error) {
	if s.deps.Reservations == nil {
		return ReturnWindow{}, nil
	}
	w, err := s.deps.Reservations.ReturnWindowFor(ctx, deviceID, from, collectingID)
	if err != nil {
		return ReturnWindow{}, fmt.Errorf("checkout: return window: %w", err)
	}
	return w, nil
}
