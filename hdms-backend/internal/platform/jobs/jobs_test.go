package jobs_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/jobs"
)

// TestParseRetentionModeDefaultsToReport pins the 5.5e fail-closed rule:
// an empty flag and empty configuration means report-only, never deletion.
func TestParseRetentionModeDefaultsToReport(t *testing.T) {
	for _, raw := range []string{"", "  "} {
		got, err := jobs.ParseRetentionMode(raw, "")
		if err != nil {
			t.Fatalf("ParseRetentionMode(%q, %q) error = %v", raw, "", err)
		}
		if got != jobs.ModeReport {
			t.Fatalf("ParseRetentionMode(%q, %q) = %q, want report", raw, "", got)
		}
	}
}

// TestParseRetentionModeAcceptsBothModes pins the explicit-configuration
// contract: only the two documented words, case-insensitive, with the flag
// overriding the configured default.
func TestParseRetentionModeAcceptsBothModes(t *testing.T) {
	cases := []struct{ raw, def, want string }{
		{"enforce", "report", "enforce"},
		{"ENFORCE", "", "enforce"},
		{"report", "enforce", "report"},
		{"", "enforce", "enforce"},
		{"", "report", "report"},
		{"  Report  ", "", "report"},
	}
	for _, c := range cases {
		got, err := jobs.ParseRetentionMode(c.raw, c.def)
		if err != nil {
			t.Fatalf("ParseRetentionMode(%q, %q) error = %v", c.raw, c.def, err)
		}
		if got != c.want {
			t.Fatalf("ParseRetentionMode(%q, %q) = %q, want %q", c.raw, c.def, got, c.want)
		}
	}
}

// TestParseRetentionModeRejectsTypos pins the fail-closed half: a misspelled
// mode is an error naming the variable, so it can never silently enable
// deletion (or silently downgrade an intended enforce to report).
func TestParseRetentionModeRejectsTypos(t *testing.T) {
	for _, raw := range []string{"enforc", "yes", "delete", "1"} {
		if got, err := jobs.ParseRetentionMode(raw, "report"); err == nil {
			t.Fatalf("ParseRetentionMode(%q) = %q, want error", raw, got)
		} else if !strings.Contains(err.Error(), "--mode") {
			t.Fatalf("ParseRetentionMode(%q) error %q does not name --mode", raw, err)
		}
	}
}

// TestTextfileContentPinsFormat pins the node_exporter exposition: a
// malformed .prom file is silently ignored by node_exporter, which would
// turn the last-success alert into a dead check.
func TestTextfileContentPinsFormat(t *testing.T) {
	ts := time.Date(2026, 9, 18, 3, 10, 0, 0, time.UTC)
	got := jobs.TextfileContent("reconcile", ts)
	want := "# HELP hdms_job_last_success Unix timestamp of the last successful run per scheduled job.\n" +
		"# TYPE hdms_job_last_success gauge\n" +
		`hdms_job_last_success{job="reconcile"} 1789701000` + "\n"
	if got != want {
		t.Fatalf("TextfileContent =\n%q\nwant\n%q", got, want)
	}
}

// TestWriteLastSuccessRoundTrip proves the atomic publish: a success lands
// a readable .prom file, and an empty dir disables the write without error.
func TestWriteLastSuccessRoundTrip(t *testing.T) {
	if err := jobs.WriteLastSuccess("", "reconcile", time.Now()); err != nil {
		t.Fatalf("WriteLastSuccess with empty dir: %v", err)
	}
	dir := t.TempDir()
	ts := time.Date(2026, 9, 18, 3, 10, 0, 0, time.UTC)
	if err := jobs.WriteLastSuccess(dir, "retention", ts); err != nil {
		t.Fatalf("WriteLastSuccess: %v", err)
	}
	// #nosec G703 -- test temp dir, fixed job filename.
	raw, err := os.ReadFile(filepath.Join(dir, "hdms_job_retention.prom"))
	if err != nil {
		t.Fatalf("read metrics file: %v", err)
	}
	if string(raw) != jobs.TextfileContent("retention", ts) {
		t.Fatalf("metrics file =\n%q\nwant\n%q", raw, jobs.TextfileContent("retention", ts))
	}
}
