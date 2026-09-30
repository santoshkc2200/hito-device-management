package jobs_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/jobs"
)

// fakeRuns stands in for job_runs: a job "writes its row" by calling started.
type fakeRuns struct {
	last map[string]time.Time
	err  error
}

func (f *fakeRuns) lastStarted(_ context.Context, names []string) (map[string]time.Time, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := map[string]time.Time{}
	for _, n := range names {
		if t, ok := f.last[n]; ok {
			out[n] = t
		}
	}
	return out, nil
}

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

var tokyo = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		panic(err)
	}
	return loc
}()

//nolint:unparam // h is fixed at 9 in current tests but kept for caller clarity
func wed(h, m int) time.Time { return time.Date(2026, 9, 30, h, m, 0, 0, tokyo) }

// recordingJob returns a job that records each run and, like the real jobs,
// writes its start time to job_runs.
func recordingJob(name string, s jobs.Schedule, runs *fakeRuns, calls *[]string, now *time.Time) jobs.Job {
	return jobs.Job{Name: name, Schedule: s, Run: func(context.Context) error {
		*calls = append(*calls, name)
		runs.last[name] = *now
		return nil
	}}
}

func TestTickRunsOnlyTheHighestPriorityDueJob(t *testing.T) {
	runs := &fakeRuns{last: map[string]time.Time{}}
	var calls []string
	now := wed(9, 0)
	w := &jobs.Worker{
		Jobs: []jobs.Job{
			recordingJob("backup", jobs.DailyAt{Hour: 2, Loc: tokyo}, runs, &calls, &now),
			recordingJob("overdue-scan", jobs.Every(time.Hour), runs, &calls, &now),
		},
		LastStarted: runs.lastStarted,
		Logger:      quietLogger(),
	}

	if got := w.Tick(context.Background(), now); got != "backup" {
		t.Fatalf("first tick ran %q, want backup", got)
	}
	now = now.Add(time.Minute)
	if got := w.Tick(context.Background(), now); got != "overdue-scan" {
		t.Fatalf("second tick ran %q, want overdue-scan", got)
	}
	now = now.Add(time.Minute)
	if got := w.Tick(context.Background(), now); got != "" {
		t.Fatalf("third tick ran %q, want nothing", got)
	}
	if len(calls) != 2 {
		t.Fatalf("calls = %v, want exactly two runs", calls)
	}
}

func TestFailedJobIsNotRetriedInTheSameWindow(t *testing.T) {
	runs := &fakeRuns{last: map[string]time.Time{}}
	attempts := 0
	w := &jobs.Worker{
		// Fails before writing a job_runs row, like a missing encryption key.
		Jobs: []jobs.Job{{Name: "backup", Schedule: jobs.DailyAt{Hour: 2, Loc: tokyo}, Run: func(context.Context) error {
			attempts++
			return errors.New("HDMS_BACKUP_ENC_KEY: missing")
		}}},
		LastStarted: runs.lastStarted,
		Logger:      quietLogger(),
	}

	for i := 0; i < 30; i++ {
		w.Tick(context.Background(), wed(9, i))
	}
	if attempts != 1 {
		t.Fatalf("attempts in one window = %d, want 1", attempts)
	}

	w.Tick(context.Background(), time.Date(2026, 10, 1, 2, 0, 0, 0, tokyo))
	if attempts != 2 {
		t.Fatalf("attempts after next window opened = %d, want 2", attempts)
	}
}

func TestMissedWindowsCatchUpOnce(t *testing.T) {
	runs := &fakeRuns{last: map[string]time.Time{
		"overdue-scan": wed(9, 0).Add(-72 * time.Hour), // host was off for three days
	}}
	var calls []string
	now := wed(9, 30)
	w := &jobs.Worker{
		Jobs:        []jobs.Job{recordingJob("overdue-scan", jobs.Every(time.Hour), runs, &calls, &now)},
		LastStarted: runs.lastStarted,
		Logger:      quietLogger(),
	}

	for i := 0; i < 20; i++ {
		w.Tick(context.Background(), now)
		now = now.Add(time.Minute)
	}
	if len(calls) != 1 {
		t.Fatalf("catch-up runs = %d, want 1", len(calls))
	}
}

func TestPanickingJobDoesNotStopTheWorker(t *testing.T) {
	runs := &fakeRuns{last: map[string]time.Time{}}
	var calls []string
	now := wed(9, 0)
	w := &jobs.Worker{
		Jobs: []jobs.Job{
			{Name: "reconcile", Schedule: jobs.DailyAt{Hour: 3, Minute: 10, Loc: tokyo}, Run: func(context.Context) error {
				var m map[string]int
				m["boom"]++ //nolint:staticcheck // nil map write panics deliberately
				return nil
			}},
			recordingJob("retention", jobs.DailyAt{Hour: 3, Minute: 40, Loc: tokyo}, runs, &calls, &now),
		},
		LastStarted: runs.lastStarted,
		Logger:      quietLogger(),
	}

	if got := w.Tick(context.Background(), now); got != "reconcile" {
		t.Fatalf("first tick = %q, want reconcile", got)
	}
	now = now.Add(time.Minute)
	if got := w.Tick(context.Background(), now); got != "retention" {
		t.Fatalf("second tick = %q, want retention", got)
	}
}

func TestTickRunsNothingWhenJobRunsIsUnreadable(t *testing.T) {
	ran := false
	w := &jobs.Worker{
		Jobs: []jobs.Job{{Name: "backup", Schedule: jobs.Every(time.Minute), Run: func(context.Context) error {
			ran = true
			return nil
		}}},
		LastStarted: (&fakeRuns{err: errors.New("connection refused")}).lastStarted,
		Logger:      quietLogger(),
	}
	if got := w.Tick(context.Background(), wed(9, 0)); got != "" || ran {
		t.Fatalf("Tick = %q, ran = %v; want nothing run while job_runs is unreadable", got, ran)
	}
}

func TestRunStopsWhenContextEnds(t *testing.T) {
	runs := &fakeRuns{last: map[string]time.Time{}}
	w := &jobs.Worker{LastStarted: runs.lastStarted, Logger: quietLogger()}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.Run(ctx, time.Hour, time.Now)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after its context was cancelled")
	}
}

func TestPendingRequestRunsBeforeScheduledJobs(t *testing.T) {
	runs := &fakeRuns{last: map[string]time.Time{}}
	var calls []string
	now := wed(9, 0)
	pending := 1
	w := &jobs.Worker{
		Jobs: []jobs.Job{recordingJob("backup", jobs.DailyAt{Hour: 2, Loc: tokyo}, runs, &calls, &now)},
		Pending: func(context.Context) bool {
			if pending == 0 {
				return false
			}
			pending--
			calls = append(calls, "request")
			return true
		},
		LastStarted: runs.lastStarted,
		Logger:      quietLogger(),
	}
	if got := w.Tick(context.Background(), now); got != "request" {
		t.Fatalf("first tick = %q, want request", got)
	}
	now = now.Add(time.Minute)
	if got := w.Tick(context.Background(), now); got != "backup" {
		t.Fatalf("second tick = %q, want backup", got)
	}
	if strings.Join(calls, ",") != "request,backup" {
		t.Fatalf("calls = %v", calls)
	}
}
