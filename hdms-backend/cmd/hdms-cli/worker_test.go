package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/config"
)

func jobNames(cfg config.Config) []string {
	var names []string
	for _, j := range scheduledJobs(cfg, time.UTC) {
		names = append(names, j.Name)
	}
	return names
}

// The names must equal what each job writes to job_runs, or the worker would
// never see a job as done and would rerun it every window.
func TestScheduledJobsPriorityAndNames(t *testing.T) {
	got := strings.Join(jobNames(config.Config{}), ",")
	want := "backup,reservation-expiry,overdue-scan,reconcile,retention,weekly-digest"
	if got != want {
		t.Fatalf("jobs without LDAP = %s, want %s", got, want)
	}

	got = strings.Join(jobNames(config.Config{LDAPURL: "ldaps://dc.hospital.local"}), ",")
	want = "backup,reservation-expiry,overdue-scan,reconcile,retention,directory-sync,weekly-digest"
	if got != want {
		t.Fatalf("jobs with LDAP = %s, want %s", got, want)
	}
}

// Schedules must match the retired deploy/systemd/*.timer files.
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
	}
	for _, j := range scheduledJobs(config.Config{LDAPURL: "ldaps://dc"}, tokyo) {
		if got := j.Schedule.Latest(now); !got.Equal(want[j.Name]) {
			t.Errorf("%s: Latest = %s, want %s", j.Name, got, want[j.Name])
		}
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
