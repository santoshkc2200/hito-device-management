package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/backup"
	"github.com/hito-hospital/hdms/internal/platform/config"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/jobs"
	"github.com/hito-hospital/hdms/internal/platform/recovery"
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

func quietStartup(connect func(context.Context) (*db.Pool, error)) *startup {
	return &startup{
		Every:   time.Millisecond,
		Kick:    make(chan struct{}, 1),
		Connect: connect,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestStartupRetriesUntilTheDatabaseAnswers(t *testing.T) {
	var calls atomic.Int32
	var s *startup
	s = quietStartup(func(context.Context) (*db.Pool, error) {
		n := calls.Add(1)
		if n == 2 && s.mode() != modeDatabaseUnavailable {
			t.Errorf("mode during the retry = %q, want %q", s.mode(), modeDatabaseUnavailable)
		}
		if n < 3 {
			return nil, errors.New("db: migrate: up: relation does not exist")
		}
		return &db.Pool{}, nil
	})
	if s.mode() != modeStarting {
		t.Fatalf("mode before run = %q, want %q", s.mode(), modeStarting)
	}
	pool, err := s.run(context.Background())
	if err != nil || pool == nil {
		t.Fatalf("run = %v, %v; want a pool", pool, err)
	}
	if calls.Load() != 3 || s.mode() != modeReady {
		t.Fatalf("calls = %d, mode = %q; want 3 and %q", calls.Load(), s.mode(), modeReady)
	}
}

func TestStartupKickRetriesAtOnce(t *testing.T) {
	var calls atomic.Int32
	s := quietStartup(func(context.Context) (*db.Pool, error) {
		if calls.Add(1) == 1 {
			return nil, errors.New("down")
		}
		return &db.Pool{}, nil
	})
	s.Every = time.Hour
	go func() {
		for s.mode() != modeDatabaseUnavailable {
			time.Sleep(time.Millisecond)
		}
		s.kick()
	}()
	done := make(chan struct{})
	go func() { _, _ = s.run(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("kick did not trigger a retry")
	}
}

// While a restore runs, the worker must not migrate: the live database may be
// mid-swap, and migrating an empty one would race the rename.
func TestStartupLeavesTheDatabaseAloneDuringARestore(t *testing.T) {
	var blocked atomic.Int32
	blocked.Store(2)
	var connects atomic.Int32
	s := quietStartup(func(context.Context) (*db.Pool, error) {
		connects.Add(1)
		return &db.Pool{}, nil
	})
	var prepares atomic.Int32
	s.Prepare = func(context.Context) error { prepares.Add(1); return nil }
	s.Blocked = func() bool { return blocked.Add(-1) >= 0 }
	if _, err := s.run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if connects.Load() != 1 || prepares.Load() != 3 {
		t.Fatalf("connects = %d, prepares = %d; want 1 and 3", connects.Load(), prepares.Load())
	}
}

func TestStartupStopsWithTheContext(t *testing.T) {
	s := quietStartup(func(context.Context) (*db.Pool, error) { return nil, errors.New("down") })
	s.Every = time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for s.mode() != modeDatabaseUnavailable {
			time.Sleep(time.Millisecond)
		}
		cancel()
	}()
	if _, err := s.run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("run = %v, want context.Canceled", err)
	}
}

// The spec's guarantee: with migrations failing, the recovery status route
// answers and says so.
func TestRecoveryStatusAnswersWhileTheDatabaseIsUnavailable(t *testing.T) {
	discard := slog.New(slog.NewTextHandler(io.Discard, nil))
	start := quietStartup(func(context.Context) (*db.Pool, error) {
		return nil, errors.New("db: migrate: up: relation \"devices\" does not exist")
	})
	start.Every = time.Hour
	engine := &recovery.Engine{
		StatePath: filepath.Join(t.TempDir(), recovery.StateFile), LiveDB: "hdms",
		Now: time.Now, Logger: discard, Base: context.Background(),
	}
	api := &recovery.Handler{
		Engine:  engine,
		Status:  func(context.Context) recovery.LiveState { return recovery.LiveDamaged },
		Worker:  start.mode,
		Sources: func(context.Context) []recovery.Source { return nil },
		Limiter: &recovery.Limiter{PerIP: 5, Total: 20, Now: time.Now},
		Now:     time.Now, Logger: discard,
	}
	srv := httptest.NewServer(workerMux(http.NotFoundHandler(), http.NotFoundHandler(), api.Routes()))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _, _ = start.run(ctx) }()

	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := http.Get(srv.URL + "/recovery/api/status")
		if err != nil {
			t.Fatal(err)
		}
		var body struct{ Database, Worker string }
		_ = json.NewDecoder(resp.Body).Decode(&body)
		_ = resp.Body.Close()
		if body.Worker == modeDatabaseUnavailable {
			if body.Database != "damaged" {
				t.Fatalf("database = %q, want damaged", body.Database)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("worker mode never became %q (last %+v)", modeDatabaseUnavailable, body)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestWorkerMuxKeepsInternalAndRecoveryApart(t *testing.T) {
	internal := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	rec := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) })
	srv := httptest.NewServer(workerMux(internal, http.NotFoundHandler(), rec))
	defer srv.Close()
	for path, want := range map[string]int{
		"/internal/locations":  http.StatusTeapot,
		"/recovery/api/status": http.StatusAccepted,
		"/recovery/index.html": http.StatusNotFound,
		"/v1/healthz":          http.StatusNotFound,
	} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("%s = %d, want %d", path, resp.StatusCode, want)
		}
	}
}
