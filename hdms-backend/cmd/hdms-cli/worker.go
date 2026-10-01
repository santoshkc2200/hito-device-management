package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/backup"
	"github.com/hito-hospital/hdms/internal/platform/config"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/jobs"
	"github.com/hito-hospital/hdms/internal/platform/recovery"
)

// workerMux serves the API-only /internal routes and the public recovery
// API on one listener. caddy routes /recovery/api/* here and never /internal.
func workerMux(internal, recoveryAPI http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/internal/", internal)
	mux.Handle("/recovery/api/", recoveryAPI)
	return mux
}

const workerTick = time.Minute

// Worker modes, as /recovery/api/status reports them.
const (
	modeStarting            = "starting"
	modeDatabaseUnavailable = "database_unavailable"
	modeReady               = "ready"
)

// dbRetry is the wait between attempts to reach and migrate the database.
const dbRetry = 30 * time.Second

var errRestoreRunning = errors.New("worker: a restore is running; the database is left alone until it ends")

// startup is the worker's path to a usable database. Until Connect succeeds
// the worker runs no jobs and writes no heartbeat, but its HTTP listener —
// and so the recovery page — stays up whatever state the database is in.
type startup struct {
	Every time.Duration
	// Kick retries at once; a finished restore uses it.
	Kick chan struct{}
	// Prepare runs before every attempt: it resumes or unwinds an
	// interrupted restore. An error is retried like a connection error.
	Prepare func(ctx context.Context) error
	// Blocked reports a restore in progress; no attempt is made meanwhile.
	Blocked func() bool
	// Connect migrates, opens the pool and provisions hdms_app.
	Connect func(ctx context.Context) (*db.Pool, error)
	Logger  *slog.Logger

	current atomic.Value // string
}

func (s *startup) mode() string {
	if m, ok := s.current.Load().(string); ok {
		return m
	}
	return modeStarting
}

func (s *startup) kick() {
	select {
	case s.Kick <- struct{}{}:
	default:
	}
}

func (s *startup) run(ctx context.Context) (*db.Pool, error) {
	for {
		pool, err := s.attempt(ctx)
		if err == nil {
			s.current.Store(modeReady)
			return pool, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		s.current.Store(modeDatabaseUnavailable)
		s.Logger.Error("worker: database unavailable; the recovery page stays up", "error", err, "retry_in", s.Every)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-s.Kick:
		case <-time.After(s.Every):
		}
	}
}

func (s *startup) attempt(ctx context.Context) (*db.Pool, error) {
	if s.Prepare != nil {
		if err := s.Prepare(ctx); err != nil {
			return nil, err
		}
	}
	if s.Blocked != nil && s.Blocked() {
		return nil, errRestoreRunning
	}
	return s.Connect(ctx)
}

// connectWorkerDB migrates as the owner, opens the pool and gives hdms_app
// its login. Any failure is retried by startup.
func connectWorkerDB(ctx context.Context, cfg config.Config, ownerURL string) (*db.Pool, error) {
	if err := db.Migrate(ctx, ownerURL); err != nil {
		return nil, err
	}
	pool, err := db.Open(ctx, ownerURL)
	if err != nil {
		return nil, err
	}
	if cfg.AppDBPassword != "" {
		if err := db.ProvisionAppRole(ctx, pool.Pool, cfg.AppDBPassword); err != nil {
			pool.Close()
			return nil, err
		}
	}
	return pool, nil
}

// workerDeps are the parts of the job table that need the database: the
// backup's live schedule and the executor-backed backup and verify runs.
type workerDeps struct {
	BackupSchedule jobs.Schedule
	RunBackup      func(ctx context.Context) error
	Verify         func(ctx context.Context) error
}

// scheduledJobs is the worker's job table, highest priority first. Backup
// leads so housekeeping never pushes it back; verify trails because it only
// reads what earlier jobs wrote.
func scheduledJobs(cfg config.Config, loc *time.Location, deps workerDeps) []jobs.Job {
	js := []jobs.Job{
		{Name: "backup", Schedule: deps.BackupSchedule, Run: deps.RunBackup},
		{Name: "reservation-expiry", Schedule: jobs.Every(5 * time.Minute),
			Run: func(ctx context.Context) error { return runReservationExpiry(ctx, cfg, nil) }},
		{Name: "overdue-scan", Schedule: jobs.Every(time.Hour),
			Run: func(ctx context.Context) error { return runOverdueScan(ctx, cfg, nil) }},
		{Name: "reconcile", Schedule: jobs.DailyAt{Hour: 3, Minute: 10, Loc: loc},
			Run: func(ctx context.Context) error { return runReconcile(ctx, cfg, nil) }},
		{Name: "retention", Schedule: jobs.DailyAt{Hour: 3, Minute: 40, Loc: loc},
			Run: func(ctx context.Context) error { return runRetention(ctx, cfg, nil) }},
	}
	if cfg.LDAPURL != "" {
		js = append(js, jobs.Job{Name: "directory-sync", Schedule: jobs.DailyAt{Hour: 3, Loc: loc},
			Run: func(ctx context.Context) error { return runDirectorySync(ctx, cfg, []string{"--apply"}) }})
	}
	return append(js,
		jobs.Job{Name: "weekly-digest", Schedule: jobs.WeeklyAt{Weekday: time.Monday, Hour: 8, Loc: loc},
			Run: func(ctx context.Context) error { return runWeeklyDigest(ctx, cfg, nil) }},
		jobs.Job{Name: "verify", Schedule: jobs.DailyAt{Hour: 4, Minute: 30, Loc: loc}, Run: deps.Verify},
	)
}

// liveSchedule reads the console-edited backup schedule on every tick. Any
// read error makes the backup not due: a flapping database must not turn
// into a backup every minute.
type liveSchedule struct {
	get func(context.Context) (backup.ScheduleConfig, error)
	loc *time.Location
	log *slog.Logger
}

func (s liveSchedule) Latest(now time.Time) time.Time {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := s.get(ctx)
	if err != nil {
		s.log.Error("worker: read backup schedule", "error", err)
		return time.Time{}
	}
	return c.JobSchedule(s.loc).Latest(now)
}

// runWorker serves its HTTP listener at once, then waits for a usable database (migrating as the owner and giving hdms_app its login), then runs the scheduled jobs until SIGTERM. The heartbeat file is first written only
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

	r, err := resticFor(cfg)
	if err != nil {
		return err
	}

	ownerURL := cfg.WorkerDatabaseURL()
	liveDB, err := recovery.DatabaseName(ownerURL)
	if err != nil {
		return fmt.Errorf("worker: %w", err)
	}
	start := &startup{
		Every:  dbRetry,
		Kick:   make(chan struct{}, 1),
		Logger: slog.Default(),
		Connect: func(ctx context.Context) (*db.Pool, error) {
			return connectWorkerDB(ctx, cfg, ownerURL)
		},
	}
	ops := &recovery.PGOps{LiveURL: ownerURL, BackupDir: cfg.BackupDir, Restic: r, Migrate: db.Migrate}
	engine := &recovery.Engine{
		StatePath: filepath.Join(cfg.BackupDir, recovery.StateFile), LiveDB: liveDB, Ops: ops,
		Now: time.Now, Logger: slog.Default(),
		Settle: 3 * time.Second, RetryDelay: time.Second,
		Finished: start.kick, Base: ctx,
	}
	start.Prepare, start.Blocked = engine.Resume, engine.Active
	recoveryAPI := &recovery.Handler{
		Engine: engine,
		Status: ops.LiveState,
		Worker: start.mode,
		Sources: func(ctx context.Context) []recovery.Source {
			return recovery.DiscoverSources(ctx, cfg.BackupDir, cfg.BackupAllowedRoots, ops.Destinations)
		},
		Snapshots: func(ctx context.Context, s recovery.Source) ([]backup.Snapshot, error) {
			return r.Snapshots(ctx, s.Repo())
		},
		Secrets: backup.NewRecoverySecrets(cfg.BackupEncKey, cfg.TokenPepper, cfg.CredentialEncKey, cfg.TOTPSecretEncKey),
		Limiter: &recovery.Limiter{PerIP: 5, Total: 20, Now: time.Now},
		Now:     time.Now,
		Logger:  slog.Default(),
	}

	// The listener starts before the database is touched: it needs only the
	// filesystem, and it serves the recovery page whatever state the
	// database is in.
	locator := &backup.Locator{BackupDir: cfg.BackupDir, AllowedRoots: cfg.BackupAllowedRoots}
	ln, err := net.Listen("tcp", cfg.WorkerHTTPAddr)
	if err != nil {
		return fmt.Errorf("worker: listen on %s: %w", cfg.WorkerHTTPAddr, err)
	}
	listener := &http.Server{Handler: workerMux(locator.InternalHandler(), recoveryAPI.Routes()), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := listener.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("worker: listener", "error", err)
		}
	}()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = listener.Shutdown(shutdownCtx)
	}()
	pool, err := start.run(ctx)
	if err != nil {
		if ctx.Err() != nil {
			slog.Info("worker: stopped before the database was ready")
			return nil
		}
		return err
	}
	defer pool.Close()

	exec := &backup.Executor{
		Pool:         pool,
		DatabaseURL:  ownerURL,
		BackupDir:    cfg.BackupDir,
		AllowedRoots: cfg.BackupAllowedRoots,
		Restic:       r,
		MetricsDir:   cfg.JobMetricsDir,
		Now:          time.Now,
		Logger:       slog.Default(),
	}
	deps := workerDeps{
		BackupSchedule: liveSchedule{
			get: func(ctx context.Context) (backup.ScheduleConfig, error) { return backup.GetSchedule(ctx, pool.Pool) },
			loc: time.Local,
			log: slog.Default(),
		},
		RunBackup: func(ctx context.Context) error {
			rep, err := exec.RunBackup(ctx)
			if err == nil && rep.Outcome != backup.OutcomeSuccess {
				err = fmt.Errorf("backup completed %s", rep.Outcome)
			}
			return err
		},
		Verify: func(ctx context.Context) error {
			if outcome, _ := exec.Verify(ctx, nil); outcome != backup.OutcomeSuccess {
				return fmt.Errorf("verify completed %s", outcome)
			}
			return nil
		},
	}

	go keepAlive(ctx, *heartbeat, 30*time.Second, func(ctx context.Context) {
		if err := backup.TouchWorker(ctx, pool.Pool, time.Now().UTC()); err != nil {
			slog.Error("worker: database heartbeat", "error", err)
		}
	})

	js := scheduledJobs(cfg, time.Local, deps)
	names := make([]string, len(js))
	for i, j := range js {
		names[i] = j.Name
	}
	slog.Info("worker: started", "timezone", time.Local.String(), "jobs", names)

	w := &jobs.Worker{
		Jobs:    js,
		Pending: exec.ProcessNext,
		Paused:  engine.Active,
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
func keepAlive(ctx context.Context, path string, every time.Duration, touchDB func(context.Context)) {
	touch := func() {
		if err := os.WriteFile(path, []byte(time.Now().UTC().Format(time.RFC3339)), 0o644); err != nil {
			slog.Error("worker: write heartbeat", "path", path, "error", err)
		}
		if touchDB != nil {
			touchDB(ctx)
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
