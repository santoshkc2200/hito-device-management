// Package domain holds lending's pure business rules — no database, no I/O
// — so they are tested without a Postgres instance (docs/03-domain-model.md).
package domain

import "time"

// DueDateFor computes a due date from a category's default loan period and
// the moment a loan was borrowed. A nil period (no default configured for
// the category) yields a nil due date — an open-ended loan.
//
// This is plain duration arithmetic: time.Time.Add operates on elapsed
// wall-clock time regardless of any DST transition the period happens to
// cross, so there is no calendar-aware logic to get wrong here.
func DueDateFor(period *time.Duration, borrowedAt time.Time) *time.Time {
	if period == nil {
		return nil
	}
	due := borrowedAt.Add(*period)
	return &due
}
