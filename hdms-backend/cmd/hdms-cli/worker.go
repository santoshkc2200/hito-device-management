package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/config"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/jobs"
)

const workerTick = time.Minute

// scheduledJobs is the worker's job table, highest priority first. Backup
// leads so housekeeping never pushes the nightly copy back. Schedules match
// the deploy/systemd/*.timer files, which Docker installs no longer use.
func scheduledJobs(cfg config.Config, loc *time.Location) []jobs.Job {
	owner := cfg
	owner.DatabaseURL = cfg.WorkerDatabaseURL()

	js := []jobs.Job{
		{Name: "backup", Schedule: jobs.DailyAt{Hour: 2, Loc: loc},
			Run: func(ctx context.Context) error { return runBackup(ctx, owner, nil) }},
		{Name: "reservation-expiry", Schedule: jobs.Every(5 * time.Minute),
			Run: func(ctx context.Context) error { return runReservationExpiry(ctx, cfg, nil) }},
		{Name: "overdue-scan", Schedule: jobs.Every(time.Hour),
			Run: func(ctx context.Context) error { return runOverdueScan(ctx, cfg, nil) }},
		{Name: "reconcile", Schedule: jobs.DailyAt{Hour: 3, Minute: 10, Loc: loc},
			Run: func(ctx context.Context) error { return runReconcile(ctx, cfg, nil) }},
		{Name: "retention", Schedule: jobs.DailyAt{Hour: 3, Minute: 40, Loc: loc},
			Run: func(ctx context.Context) error { return runRetention(ctx, cfg, nil) }},
	}
	// Without a directory there is nothing to sync; registering the job
	// would only write a failure row every night.
	if cfg.LDAPURL != "" {
		js = append(js, jobs.Job{Name: "directory-sync", Schedule: jobs.DailyAt{Hour: 3, Loc: loc},
			Run: func(ctx context.Context) error { return runDirectorySync(ctx, cfg, []string{"--apply"}) }})
	}
	return append(js, jobs.Job{Name: "weekly-digest", Schedule: jobs.WeeklyAt{Weekday: time.Monday, Hour: 8, Loc: loc},
		Run: func(ctx context.Context) error { return runWeeklyDigest(ctx, cfg, nil) }})
}

// runWorker migrates as the owner, gives hdms_app its login, then runs the
// scheduled jobs until SIGTERM. The heartbeat file is first written only
// after startup succeeds, so compose starts the API once the schema exists.
func runWorker(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("worker", flag.ContinueOnError)
	heartbeat := fs.String("heartbeat-file", "/tmp/hdms-worker-alive", "file touched every 30s while the worker runs")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("usage: hdms-cli worker [--heartbeat-file <path>]")
	}

	if cfg.Env == "production" {
		if cfg.OwnerDatabaseURL == "" {
			return errors.New("HDMS_OWNER_DATABASE_URL: required for the worker in production; it runs migrations and backups as the database owner")
		}
		if os.Getenv("TZ") == "" {
			return errors.New("TZ: required for the worker in production; scheduled jobs run at local wall-clock times (for example TZ=Asia/Tokyo)")
		}
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	ownerURL := cfg.WorkerDatabaseURL()
	if err := db.Migrate(ctx, ownerURL); err != nil {
		return err
	}
	pool, err := db.Open(ctx, ownerURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	if cfg.AppDBPassword != "" {
		if err := db.ProvisionAppRole(ctx, pool.Pool, cfg.AppDBPassword); err != nil {
			return err
		}
	}

	go keepAlive(ctx, *heartbeat, 30*time.Second)

	js := scheduledJobs(cfg, time.Local)
	names := make([]string, len(js))
	for i, j := range js {
		names[i] = j.Name
	}
	slog.Info("worker: started", "timezone", time.Local.String(), "jobs", names)

	w := &jobs.Worker{
		Jobs: js,
		LastStarted: func(ctx context.Context, names []string) (map[string]time.Time, error) {
			return jobs.LastStarted(ctx, pool.Pool, names)
		},
		Logger: slog.Default(),
	}
	w.Run(ctx, workerTick, time.Now)
	slog.Info("worker: stopped")
	return nil
}

// keepAlive rewrites path every interval until ctx ends. The compose
// healthcheck treats a file older than three minutes as a dead worker.
func keepAlive(ctx context.Context, path string, every time.Duration) {
	touch := func() {
		if err := os.WriteFile(path, []byte(time.Now().UTC().Format(time.RFC3339)), 0o644); err != nil {
			slog.Error("worker: write heartbeat", "path", path, "error", err)
		}
	}
	touch()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			touch()
		}
	}
}
