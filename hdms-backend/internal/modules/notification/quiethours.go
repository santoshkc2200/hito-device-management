package notification

import (
	"time"
)

// QuietHoursConfig defines the hospital quiet-hours window (6.2d).
// Clinical courtesy: night staff should not receive reminder emails at 03:00.
// Messages arriving during this window are queued for the opening time, never dropped.
type QuietHoursConfig struct {
	Enabled   bool
	StartHour int // e.g. 21 (21:00 / 9 PM)
	EndHour   int // e.g. 7 (07:00 / 7 AM)
	Location  *time.Location
}

// DefaultQuietHoursConfig provides the clinical standard (21:00 - 07:00 local time).
func DefaultQuietHoursConfig() QuietHoursConfig {
	loc, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		loc = time.Local
	}
	return QuietHoursConfig{
		Enabled:   true,
		StartHour: 21,
		EndHour:   7,
		Location:  loc,
	}
}

// IsQuiet reports whether instant t falls within the quiet hours window.
func (q QuietHoursConfig) IsQuiet(t time.Time) bool {
	if !q.Enabled {
		return false
	}
	loc := q.Location
	if loc == nil {
		loc = time.Local
	}
	localT := t.In(loc)
	hour := localT.Hour()

	if q.StartHour > q.EndHour {
		// Window crosses midnight (e.g., 21:00 to 07:00)
		return hour >= q.StartHour || hour < q.EndHour
	}
	// Window within the same calendar day
	return hour >= q.StartHour && hour < q.EndHour
}

// NextWindowOpen returns the earliest time at or after t when quiet hours end.
func (q QuietHoursConfig) NextWindowOpen(t time.Time) time.Time {
	loc := q.Location
	if loc == nil {
		loc = time.Local
	}
	localT := t.In(loc)
	hour := localT.Hour()

	if !q.IsQuiet(t) {
		return t.UTC()
	}

	if q.StartHour > q.EndHour {
		if hour >= q.StartHour {
			// Before midnight: window opens tomorrow at EndHour:00
			tomorrow := localT.AddDate(0, 0, 1)
			return time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), q.EndHour, 0, 0, 0, loc).UTC()
		}
		// After midnight: window opens today at EndHour:00
		return time.Date(localT.Year(), localT.Month(), localT.Day(), q.EndHour, 0, 0, 0, loc).UTC()
	}

	// Same-day window
	return time.Date(localT.Year(), localT.Month(), localT.Day(), q.EndHour, 0, 0, 0, loc).UTC()
}
