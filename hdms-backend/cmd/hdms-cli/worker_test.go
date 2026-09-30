package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/backup"
	"github.com/hito-hospital/hdms/internal/platform/config"
	"github.com/hito-hospital/hdms/internal/platform/jobs"
)

func testDeps(loc *time.Location) workerDeps {
	return workerDeps{
		BackupSchedule: jobs.DailyAt{Hour: 2, Loc: loc},
		RunBackup:      func(context.Context) error { return nil },
		Verify:         func(context.Context) error { return nil },
	}
}

func jobNames(cfg config.Config) []string {
	var names []string
	for _, j := range scheduledJobs(cfg, time.UTC, testDeps(time.UTC)) {
		names = append(names, j.Name)
	}
	return names
}

func TestScheduledJobsPriorityAndNames(t *testing.T) {
	got := strings.Join(jobNames(config.Config{}), ",")
	want := "backup,reservation-expiry,overdue-scan,reconcile,retention,weekly-digest,verify"
	if got != want {
		t.Fatalf("jobs without LDAP = %s, want %s", got, want)
	}
	got = strings.Join(jobNames(config.Config{LDAPURL: "ldaps://dc.hospital.local"}), ",")
	want = "backup,reservation-expiry,overdue-scan,reconcile,retention,directory-sync,weekly-digest,verify"
	if got != want {
		t.Fatalf("jobs with LDAP = %s, want %s", got, want)
	}
}

func TestScheduledJobsMatchRetiredTimers(t *testing.T) {
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 30, 12, 34, 0, 0, tokyo) // Wednesday
	want := map[string]time.Time{
		"backup":             time.Date(2026, 9, 30, 2, 0, 0, 0, tokyo),
		"reservation-expiry": time.Date(2026, 9, 30, 12, 30, 0, 0, tokyo),
		"overdue-scan":       time.Date(2026, 9, 30, 12, 0, 0, 0, tokyo),
		"reconcile":          time.Date(2026, 9, 30, 3, 10, 0, 0, tokyo),
		"retention":          time.Date(2026, 9, 30, 3, 40, 0, 0, tokyo),
		"directory-sync":     time.Date(2026, 9, 30, 3, 0, 0, 0, tokyo),
		"weekly-digest":      time.Date(2026, 9, 28, 8, 0, 0, 0, tokyo),
		"verify":             time.Date(2026, 9, 30, 4, 30, 0, 0, tokyo),
	}
	for _, j := range scheduledJobs(config.Config{LDAPURL: "ldaps://dc"}, tokyo, testDeps(tokyo)) {
		if got := j.Schedule.Latest(now); !got.Equal(want[j.Name]) {
			t.Errorf("%s: Latest = %s, want %s", j.Name, got, want[j.Name])
		}
	}
}

// The backup schedule comes from the database; a read error must make the
// backup "not due", never "due every minute".
func TestLiveScheduleFollowsDatabase(t *testing.T) {
	tokyo, _ := time.LoadLocation("Asia/Tokyo")
	now := time.Date(2026, 9, 30, 12, 34, 0, 0, tokyo)
	cfg := backup.ScheduleConfig{Enabled: true, Mode: "daily", TimeLocal: "05:00"}
	var err error
	s := liveSchedule{
		get: func(context.Context) (backup.ScheduleConfig, error) { return cfg, err },
		loc: tokyo,
		log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	if got := s.Latest(now); !got.Equal(time.Date(2026, 9, 30, 5, 0, 0, 0, tokyo)) {
		t.Fatalf("Latest = %s", got)
	}
	cfg.Enabled = false
	if got := s.Latest(now); !got.IsZero() {
		t.Fatalf("disabled Latest = %s, want zero", got)
	}
	cfg.Enabled, err = true, errors.New("db down")
	if got := s.Latest(now); !got.IsZero() {
		t.Fatalf("Latest on read error = %s, want zero", got)
	}
}

func TestRunWorkerRefusesProductionWithoutOwnerURL(t *testing.T) {
	t.Setenv("TZ", "Asia/Tokyo")
	err := runWorker(context.Background(), config.Config{Env: "production", DatabaseURL: "postgres://x"}, nil)
	if err == nil || !strings.Contains(err.Error(), "HDMS_OWNER_DATABASE_URL") {
		t.Fatalf("err = %v, want one naming HDMS_OWNER_DATABASE_URL", err)
	}
}

func TestRunWorkerRefusesProductionWithoutTZ(t *testing.T) {
	t.Setenv("TZ", "")
	err := runWorker(context.Background(), config.Config{
		Env: "production", DatabaseURL: "postgres://x", OwnerDatabaseURL: "postgres://owner",
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "TZ") {
		t.Fatalf("err = %v, want one naming TZ", err)
	}
}
