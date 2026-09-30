package jobs_test

import (
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/jobs"
)

func mustLoad(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("load %s: %v", name, err)
	}
	return loc
}

// 2026-09-30 is a Wednesday.
func TestScheduleLatest(t *testing.T) {
	tokyo := mustLoad(t, "Asia/Tokyo")
	at := func(y int, m time.Month, d, h, min int) time.Time {
		return time.Date(y, m, d, h, min, 0, 0, tokyo)
	}

	cases := []struct {
		name string
		s    jobs.Schedule
		now  time.Time
		want time.Time
	}{
		{"every 5m mid-window", jobs.Every(5 * time.Minute), at(2026, 9, 30, 10, 7).Add(30 * time.Second), at(2026, 9, 30, 10, 5)},
		{"every 5m on boundary", jobs.Every(5 * time.Minute), at(2026, 9, 30, 10, 5), at(2026, 9, 30, 10, 5)},
		{"hourly", jobs.Every(time.Hour), at(2026, 9, 30, 10, 59), at(2026, 9, 30, 10, 0)},
		{"daily before today's time", jobs.DailyAt{Hour: 2, Loc: tokyo}, at(2026, 9, 30, 1, 59), at(2026, 9, 29, 2, 0)},
		{"daily exactly at time", jobs.DailyAt{Hour: 2, Loc: tokyo}, at(2026, 9, 30, 2, 0), at(2026, 9, 30, 2, 0)},
		{"daily after time", jobs.DailyAt{Hour: 3, Minute: 10, Loc: tokyo}, at(2026, 9, 30, 23, 0), at(2026, 9, 30, 3, 10)},
		{"weekly later in week", jobs.WeeklyAt{Weekday: time.Monday, Hour: 8, Loc: tokyo}, at(2026, 9, 30, 12, 0), at(2026, 9, 28, 8, 0)},
		{"weekly same day before time", jobs.WeeklyAt{Weekday: time.Monday, Hour: 8, Loc: tokyo}, at(2026, 9, 28, 7, 59), at(2026, 9, 21, 8, 0)},
		{"weekly same day after time", jobs.WeeklyAt{Weekday: time.Monday, Hour: 8, Loc: tokyo}, at(2026, 9, 28, 8, 1), at(2026, 9, 28, 8, 0)},
		{"daily given UTC now", jobs.DailyAt{Hour: 2, Loc: tokyo}, time.Date(2026, 9, 29, 17, 30, 0, 0, time.UTC), at(2026, 9, 30, 2, 0)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := c.s.Latest(c.now)
			if !got.Equal(c.want) {
				t.Fatalf("Latest(%s) = %s, want %s", c.now, got, c.want)
			}
		})
	}
}

// A daily time that does not exist on a DST spring-forward day must still
// produce an instant at or before now, not a future one.
func TestDailyAtAcrossDSTGap(t *testing.T) {
	ny := mustLoad(t, "America/New_York")
	now := time.Date(2026, 3, 8, 4, 0, 0, 0, ny) // DST began 02:00 → 03:00
	got := jobs.DailyAt{Hour: 2, Minute: 30, Loc: ny}.Latest(now)
	if got.After(now) {
		t.Fatalf("Latest = %s, after now %s", got, now)
	}
	if got.Day() != 8 {
		t.Fatalf("Latest = %s, want a time on 2026-03-08", got)
	}
}

func TestDue(t *testing.T) {
	tokyo := mustLoad(t, "Asia/Tokyo")
	daily := jobs.DailyAt{Hour: 2, Loc: tokyo}
	now := time.Date(2026, 9, 30, 9, 0, 0, 0, tokyo)
	today := time.Date(2026, 9, 30, 2, 0, 0, 0, tokyo)

	if !jobs.Due(daily, now, time.Time{}) {
		t.Fatal("never-run job must be due")
	}
	if !jobs.Due(daily, now, today.Add(-time.Minute)) {
		t.Fatal("job last started before today's window must be due")
	}
	if jobs.Due(daily, now, today) {
		t.Fatal("job started exactly at the window must not be due again")
	}
	if jobs.Due(daily, now, today.Add(3*time.Hour)) {
		t.Fatal("job started after the window must not be due")
	}
}
