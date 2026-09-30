//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/hito-hospital/hdms/internal/platform/backup"
	"github.com/hito-hospital/hdms/test/testdb"
)

func TestScheduleDefaultsAndSave(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	got, err := backup.GetSchedule(ctx, pool.Pool)
	if err != nil {
		t.Fatalf("GetSchedule: %v", err)
	}
	if !got.Enabled || got.Mode != "daily" || got.TimeLocal != "02:00" {
		t.Fatalf("defaults = %+v, want enabled daily 02:00", got)
	}

	saved, err := backup.SaveSchedule(ctx, pool.Pool, backup.ScheduleConfig{
		Enabled: true, Mode: "weekly", IntervalMinutes: 360, TimeLocal: "03:15", Weekday: 5,
	}, "admin:test")
	if err != nil {
		t.Fatalf("SaveSchedule: %v", err)
	}
	if saved.Mode != "weekly" || saved.Weekday != 5 || saved.UpdatedBy != "admin:test" {
		t.Fatalf("saved = %+v", saved)
	}

	if _, err := backup.SaveSchedule(ctx, pool.Pool, backup.ScheduleConfig{Mode: "daily", TimeLocal: "99:00", IntervalMinutes: 360}, "admin:test"); err == nil {
		t.Fatal("invalid schedule saved")
	}
}
