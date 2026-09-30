package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// Job is one scheduled unit of work the worker runs.
type Job struct {
	Name     string
	Schedule Schedule
	Run      func(ctx context.Context) error
}

// Worker runs at most one due job per Tick. Jobs earlier in the slice win
// when several are due, so a backup is never queued behind housekeeping.
//
// "Due" compares a job's latest scheduled instant with the later of its
// newest job_runs start and the worker's own last attempt. The attempt memo
// is what stops a job that fails before writing job_runs (a missing key, an
// unreachable server) from being retried every minute.
type Worker struct {
	Jobs        []Job
	LastStarted func(ctx context.Context, names []string) (map[string]time.Time, error)
	Logger      *slog.Logger

	attempted map[string]time.Time
}

// Tick runs the first due job and returns its name, or "" when nothing ran.
func (w *Worker) Tick(ctx context.Context, now time.Time) string {
	if w.attempted == nil {
		w.attempted = map[string]time.Time{}
	}
	names := make([]string, len(w.Jobs))
	for i, j := range w.Jobs {
		names[i] = j.Name
	}
	last, err := w.LastStarted(ctx, names)
	if err != nil {
		w.Logger.Error("worker: read job_runs", "error", err)
		return ""
	}
	for _, j := range w.Jobs {
		since := last[j.Name]
		if a := w.attempted[j.Name]; a.After(since) {
			since = a
		}
		if !Due(j.Schedule, now, since) {
			continue
		}
		w.attempted[j.Name] = now
		w.run(ctx, j)
		return j.Name
	}
	return ""
}

func (w *Worker) run(ctx context.Context, j Job) {
	defer func() {
		if r := recover(); r != nil {
			w.Logger.Error("worker: job panicked", "job", j.Name, "panic", fmt.Sprint(r))
		}
	}()
	w.Logger.Info("worker: job starting", "job", j.Name)
	if err := j.Run(ctx); err != nil {
		w.Logger.Error("worker: job failed", "job", j.Name, "error", err)
		return
	}
	w.Logger.Info("worker: job finished", "job", j.Name)
}

// Run ticks every interval until ctx ends. A job in progress receives the
// cancelled context and is expected to stop; a window it did not finish is
// caught up on the next start.
func (w *Worker) Run(ctx context.Context, interval time.Duration, now func() time.Time) {
	for {
		if ctx.Err() != nil {
			return
		}
		w.Tick(ctx, now())
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}
