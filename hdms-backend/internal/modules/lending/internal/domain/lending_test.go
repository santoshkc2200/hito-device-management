package domain

import (
	"testing"
	"time"
)

func TestDueDateFor(t *testing.T) {
	borrowed := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)

	if got := DueDateFor(nil, borrowed); got != nil {
		t.Fatalf("DueDateFor(nil period) = %v, want nil", got)
	}

	period := 48 * time.Hour
	got := DueDateFor(&period, borrowed)
	if got == nil {
		t.Fatal("DueDateFor(48h) = nil, want a due date")
	}
	want := borrowed.Add(48 * time.Hour)
	if !got.Equal(want) {
		t.Fatalf("DueDateFor(48h) = %v, want %v", got, want)
	}
}

// TestDueDateForCrossesDST proves the period is elapsed wall-clock time, not
// calendar days: a loan borrowed just before a US spring-forward transition
// with a 24h period is due exactly 24 elapsed hours later, i.e. 23:00 the
// next day local time, not "the same clock time the next day".
func TestDueDateForCrossesDST(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	// 2026-03-08 is the US spring-forward date (clocks jump 02:00 -> 03:00).
	borrowed := time.Date(2026, 3, 7, 12, 0, 0, 0, loc)
	period := 24 * time.Hour

	got := DueDateFor(&period, borrowed)
	if got == nil {
		t.Fatal("DueDateFor(24h) = nil, want a due date")
	}
	if !got.Equal(borrowed.Add(24 * time.Hour)) {
		t.Fatalf("DueDateFor(24h) = %v, want exactly 24 elapsed hours after %v", got, borrowed)
	}
	// The elapsed-time due date lands at 13:00 local, not 12:00, because the
	// clock skipped an hour in between — the calendar-naive answer would be
	// wrong here.
	if got.Hour() != 13 {
		t.Fatalf("due date local hour = %d, want 13 (24 elapsed hours across a spring-forward transition)", got.Hour())
	}
}
