package backup_test

import (
	"errors"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/backup"
)

var tokyo = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		panic(err)
	}
	return loc
}()

func TestScheduleValidate(t *testing.T) {
	ok := []backup.ScheduleConfig{
		{Enabled: true, Mode: "daily", TimeLocal: "02:00", IntervalMinutes: 360},
		{Enabled: true, Mode: "weekly", TimeLocal: "23:59", Weekday: 6, IntervalMinutes: 360},
		{Enabled: true, Mode: "interval", IntervalMinutes: 15, TimeLocal: "02:00"},
		{Enabled: false, Mode: "interval", IntervalMinutes: 720, TimeLocal: "00:00"},
	}
	for _, c := range ok {
		if err := c.Validate(); err != nil {
			t.Errorf("%+v: unexpected error %v", c, err)
		}
	}
	bad := []backup.ScheduleConfig{
		{Mode: "hourly", TimeLocal: "02:00", IntervalMinutes: 360},
		{Mode: "interval", IntervalMinutes: 14, TimeLocal: "02:00"},
		{Mode: "interval", IntervalMinutes: 721, TimeLocal: "02:00"},
		{Mode: "daily", TimeLocal: "24:00", IntervalMinutes: 360},
		{Mode: "daily", TimeLocal: "2:00", IntervalMinutes: 360},
		{Mode: "weekly", TimeLocal: "02:00", Weekday: 7, IntervalMinutes: 360},
		{Mode: "weekly", TimeLocal: "02:00", Weekday: -1, IntervalMinutes: 360},
	}
	for _, c := range bad {
		if err := c.Validate(); !errors.Is(err, backup.ErrInvalidSchedule) {
			t.Errorf("%+v: err = %v, want ErrInvalidSchedule", c, err)
		}
	}
}

// 2026-09-30 is a Wednesday.
func TestScheduleNextAfter(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 34, 0, 0, tokyo)
	cases := []struct {
		c    backup.ScheduleConfig
		want time.Time
	}{
		{backup.ScheduleConfig{Enabled: true, Mode: "daily", TimeLocal: "02:00"}, time.Date(2026, 10, 1, 2, 0, 0, 0, tokyo)},
		{backup.ScheduleConfig{Enabled: true, Mode: "daily", TimeLocal: "18:30"}, time.Date(2026, 9, 30, 18, 30, 0, 0, tokyo)},
		{backup.ScheduleConfig{Enabled: true, Mode: "weekly", TimeLocal: "08:00", Weekday: 1}, time.Date(2026, 10, 5, 8, 0, 0, 0, tokyo)},
		{backup.ScheduleConfig{Enabled: true, Mode: "interval", IntervalMinutes: 60}, time.Date(2026, 9, 30, 13, 0, 0, 0, tokyo)},
	}
	for _, c := range cases {
		got, ok := c.c.NextAfter(now, tokyo)
		if !ok || !got.Equal(c.want) {
			t.Errorf("%+v: NextAfter = %s,%v want %s", c.c, got, ok, c.want)
		}
	}
}

func TestDisabledScheduleIsNeverDue(t *testing.T) {
	c := backup.ScheduleConfig{Enabled: false, Mode: "daily", TimeLocal: "02:00"}
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, tokyo)
	if _, ok := c.NextAfter(now, tokyo); ok {
		t.Fatal("disabled schedule reported a next run")
	}
	// A job that has never run is due under any enabled schedule; a disabled
	// one must still not be.
	if latest := c.JobSchedule(tokyo).Latest(now); !latest.IsZero() {
		t.Fatalf("disabled Latest = %s, want zero time", latest)
	}
}

func TestJobScheduleMatchesMode(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 34, 0, 0, tokyo)
	daily := backup.ScheduleConfig{Enabled: true, Mode: "daily", TimeLocal: "02:00"}.JobSchedule(tokyo).Latest(now)
	if !daily.Equal(time.Date(2026, 9, 30, 2, 0, 0, 0, tokyo)) {
		t.Errorf("daily latest = %s", daily)
	}
	interval := backup.ScheduleConfig{Enabled: true, Mode: "interval", IntervalMinutes: 30}.JobSchedule(tokyo).Latest(now)
	if !interval.Equal(time.Date(2026, 9, 30, 12, 30, 0, 0, tokyo)) {
		t.Errorf("interval latest = %s", interval)
	}
}
