package recovery

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hito-hospital/hdms/internal/platform/backup"
)

func testConsole(t *testing.T) (*Client, *Engine, *fakeOps) {
	t.Helper()
	e, ops := newTestEngine(t)
	h := &ConsoleHandler{
		Engine: e,
		Source: func(_ context.Context, repo string) (Source, error) {
			if repo != "local" {
				return Source{}, ErrSourceNotFound
			}
			return Source{ID: "local", Kind: SourceLocal, Folder: "/backups"}, nil
		},
		Snapshots: func(context.Context, Source) ([]backup.Snapshot, error) {
			return []backup.Snapshot{{ID: "snap1", Time: testTaken}}, nil
		},
		Logger: e.Logger,
	}
	srv := httptest.NewServer(h.Routes())
	t.Cleanup(srv.Close)
	return NewClient(srv.URL), e, ops
}

func TestConsoleRestoreUndoDiscardRoundTrip(t *testing.T) {
	c, e, ops := testConsole(t)
	ctx := context.Background()

	v, err := c.State(ctx)
	if err != nil || v != nil {
		t.Fatalf("State before any restore = %+v, %v", v, err)
	}
	v, err = c.Start(ctx, "local", "snap1", "admin:7")
	if err != nil || v == nil || v.Kind != KindRestore {
		t.Fatalf("Start = %+v, %v", v, err)
	}
	e.Wait()
	if st, _ := e.State(); st.RequestedBy != "admin:7" || st.SnapshotTakenAt != testTaken {
		t.Fatalf("engine state = %+v", st)
	}
	v, _ = c.State(ctx)
	if v.Phase != PhaseCompleted || !v.CanUndo || !v.CanDiscard {
		t.Fatalf("State after restore = %+v", v)
	}

	if _, err := c.Undo(ctx, "admin:8"); err != nil {
		t.Fatal(err)
	}
	e.Wait()
	if st, _ := e.State(); st.Kind != KindUndo || st.RequestedBy != "admin:8" {
		t.Fatalf("after undo = %+v", st)
	}

	v, err = c.Discard(ctx, "admin:8")
	if err != nil || v.CanDiscard || v.DiscardedAt == nil {
		t.Fatalf("Discard = %+v, %v", v, err)
	}
	if ops.has("hdms_rolledback_20261001t090000") {
		t.Fatal("rolled-back database not dropped")
	}
}

func TestConsoleErrorsComeBackAsSentinels(t *testing.T) {
	c, _, _ := testConsole(t)
	ctx := context.Background()
	cases := []struct {
		name string
		call func() error
		want error
	}{
		{"unknown repo", func() error { _, err := c.Start(ctx, "nope", "snap1", "admin:7"); return err }, ErrSourceNotFound},
		{"unknown snapshot", func() error { _, err := c.Start(ctx, "local", "snapX", "admin:7"); return err }, ErrSnapshotNotFound},
		{"nothing to undo", func() error { _, err := c.Undo(ctx, "admin:7"); return err }, ErrNothingToUndo},
		{"nothing to discard", func() error { _, err := c.Discard(ctx, "admin:7"); return err }, ErrNothingToDiscard},
	}
	for _, tc := range cases {
		if err := tc.call(); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
}

func TestConsoleStartWhileRunningIs409(t *testing.T) {
	c, e, ops := testConsole(t)
	ops.block = make(chan struct{})
	if _, err := c.Start(context.Background(), "local", "snap1", "admin:7"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Start(context.Background(), "local", "snap1", "admin:8"); !errors.Is(err, ErrRestoreActive) {
		t.Fatalf("second Start = %v, want ErrRestoreActive", err)
	}
	close(ops.block)
	e.Wait()
}

func TestConsoleStartWithoutRequesterIsRefused(t *testing.T) {
	c, _, _ := testConsole(t)
	if _, err := c.Start(context.Background(), "local", "snap1", ""); err == nil {
		t.Fatal("a restore without a requester was accepted")
	}
}

func TestClientReportsAnUnreachableWorker(t *testing.T) {
	c := NewClient("http://127.0.0.1:1")
	c.HTTP.Timeout = time.Second
	if _, err := c.State(context.Background()); !errors.Is(err, backup.ErrWorkerUnavailable) {
		t.Fatalf("State = %v, want ErrWorkerUnavailable", err)
	}
	var nilClient *Client
	if _, err := nilClient.State(context.Background()); !errors.Is(err, backup.ErrWorkerUnavailable) {
		t.Fatalf("nil client State = %v", err)
	}
}

func TestConsoleSource(t *testing.T) {
	// Resolve checks paths with symlinks followed; macOS temp dirs are links.
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(root, "nas", "hdms")
	if err := os.MkdirAll(filepath.Join(folder, "repo"), 0o750); err != nil {
		t.Fatal(err)
	}
	pathID, cloudID := uuid.New(), uuid.New()
	dests := func(context.Context) ([]backup.Destination, error) {
		return []backup.Destination{
			{ID: pathID, Name: "Ward NAS", Kind: "path", Target: folder},
			{ID: cloudID, Name: "Drive", Kind: "rclone", Target: "acct:hdms"},
		}, nil
	}
	ctx := context.Background()

	local, err := ConsoleSource(ctx, "local", "/var/backups/hdms", []string{root}, dests)
	if err != nil || local.Kind != SourceLocal || local.Folder != "/var/backups/hdms" {
		t.Fatalf("local = %+v, %v", local, err)
	}
	nas, err := ConsoleSource(ctx, pathID.String(), "/var/backups/hdms", []string{root}, dests)
	if err != nil || nas.Kind != SourceDestination || nas.Name != "Ward NAS" || nas.Repo().Location != filepath.Join(folder, "repo") {
		t.Fatalf("path destination = %+v, %v", nas, err)
	}
	if _, err := ConsoleSource(ctx, cloudID.String(), "/var/backups/hdms", []string{root}, dests); !errors.Is(err, ErrSourceUnsupported) {
		t.Fatalf("cloud destination err = %v, want ErrSourceUnsupported", err)
	}
	if _, err := ConsoleSource(ctx, uuid.NewString(), "/var/backups/hdms", []string{root}, dests); !errors.Is(err, ErrSourceNotFound) {
		t.Fatalf("unknown id err = %v, want ErrSourceNotFound", err)
	}
	if _, err := ConsoleSource(ctx, pathID.String(), "/var/backups/hdms", []string{"/elsewhere"}, dests); !errors.Is(err, ErrSourceNotFound) {
		t.Fatalf("destination outside the roots err = %v, want ErrSourceNotFound", err)
	}
}
