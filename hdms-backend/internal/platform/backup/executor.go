package backup

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	backupstore "github.com/hito-hospital/hdms/internal/platform/backup/store"
	"github.com/hito-hospital/hdms/internal/platform/db"
)

// verifyReadPercent is how much repository data a verify reads: enough to
// catch bit rot in real blobs, bounded so a daily run stays short.
const verifyReadPercent = 5

// Executor runs backup work inside the worker, where the real mounts and
// binaries are. The API never calls it.
type Executor struct {
	Pool         *db.Pool
	DatabaseURL  string // owner DSN
	BackupDir    string
	AllowedRoots []string
	Restic       Restic
	MetricsDir   string
	Now          func() time.Time
	Logger       *slog.Logger
	// Cloud and Rclone let the worker reach Google Drive and OneDrive
	// destinations. Without Cloud, cloud destinations fail with a plain message.
	Cloud  *CloudService
	Rclone Rclone
}

// openCloud renders the rclone settings for every cloud destination. It
// returns nil when there is nothing to render or it cannot (logged); cloud
// destinations then fail individually instead of stopping the run.
func (e *Executor) openCloud(ctx context.Context) *CloudSession {
	if e.Cloud == nil {
		return nil
	}
	dests, err := LoadAllDestinations(ctx, e.Pool)
	if err != nil {
		e.Logger.Error("backup: list destinations for cloud session", "error", err)
		return nil
	}
	sess, err := e.Cloud.OpenSession(ctx, dests, e.Rclone)
	if err != nil {
		e.Logger.Error("backup: prepare cloud accounts", "error", err)
		return nil
	}
	return sess
}

// RunBackup backs up to the local repository and every enabled destination,
// then refreshes the snapshot cache.
func (e *Executor) RunBackup(ctx context.Context) (RunReport, error) {
	dests, err := LoadEnabledDestinations(ctx, e.Pool)
	if err != nil {
		return RunReport{Outcome: OutcomeFailure}, err
	}
	bundle, err := LoadRecoveryBundle(ctx, e.Pool.Pool)
	if err != nil {
		// A missing bundle must not stop a backup; log and carry on.
		e.Logger.Error("backup: load recovery bundle", "error", err)
	}
	sess := e.openCloud(ctx)
	rep, err := RunBackup(ctx, Options{
		Pool:           e.Pool,
		DatabaseURL:    e.DatabaseURL,
		BackupDir:      e.BackupDir,
		AllowedRoots:   e.AllowedRoots,
		Restic:         e.Restic,
		Destinations:   dests,
		RecoveryBundle: bundle,
		MetricsDir:     e.MetricsDir,
		Cloud:          sess,
	}, e.Now().UTC())
	sess.Close(ctx)
	e.RefreshSnapshots(ctx)
	return rep, err
}

type repoTarget struct {
	key    string
	name   string
	repo   Repo
	restic Restic
}

// targets lists the local repository plus every enabled destination, or just
// the one destination named by only. Each target carries the restic client to
// use, which for a cloud destination points at the session's rclone config.
func (e *Executor) targets(ctx context.Context, only *uuid.UUID, sess *CloudSession) ([]repoTarget, []map[string]any, error) {
	var out []repoTarget
	var problems []map[string]any
	if only == nil {
		out = append(out, repoTarget{key: LocalRepoKey, name: "local", repo: LocalRepo(e.BackupDir), restic: e.Restic})
	}
	dests, err := LoadAllDestinations(ctx, e.Pool)
	if err != nil {
		return nil, nil, err
	}
	for _, d := range dests {
		if only != nil && d.ID != *only {
			continue
		}
		if only == nil && !d.Enabled {
			continue
		}
		problem := func(err error) {
			problems = append(problems, map[string]any{"name": d.Name, "outcome": OutcomeFailure, "error": err.Error()})
		}
		if err := sess.Usable(d); err != nil {
			problem(err)
			continue
		}
		repo, err := d.Resolve(e.AllowedRoots)
		if err != nil {
			problem(err)
			continue
		}
		restic := e.Restic
		if d.CloudAccountID != nil {
			restic = sess.Apply(restic)
		}
		out = append(out, repoTarget{key: RepoKey(d.ID), name: d.Name, repo: repo, restic: restic})
	}
	return out, problems, nil
}

// RefreshSnapshots re-reads every repository's snapshot list into the cache.
// A repository that cannot be listed keeps its previous cache.
func (e *Executor) RefreshSnapshots(ctx context.Context) {
	sess := e.openCloud(ctx)
	defer sess.Close(ctx)
	targets, _, err := e.targets(ctx, nil, sess)
	if err != nil {
		e.Logger.Error("backup: list repositories for cache refresh", "error", err)
		return
	}
	for _, t := range targets {
		snaps, err := t.restic.Snapshots(ctx, t.repo)
		if err != nil {
			e.Logger.Warn("backup: list snapshots", "repository", t.name, "error", err)
			continue
		}
		if err := ReplaceSnapshots(ctx, e.Pool.Pool, t.key, snaps, e.Now().UTC()); err != nil {
			e.Logger.Error("backup: cache snapshots", "repository", t.name, "error", err)
		}
	}
}

// Verify runs restic check on the local repository and enabled destinations
// (or on one destination) and records a job_runs row "verify".
func (e *Executor) Verify(ctx context.Context, only *uuid.UUID) (string, map[string]any) {
	started := e.Now().UTC()
	sess := e.openCloud(ctx)
	defer sess.Close(ctx)
	targets, results, err := e.targets(ctx, only, sess)
	if err != nil {
		detail := map[string]any{"error": err.Error()}
		_ = RecordJobRun(ctx, e.Pool.Pool, "verify", started, e.Now().UTC(), OutcomeFailure, detail)
		return OutcomeFailure, detail
	}
	e.RefreshSnapshots(ctx)
	failed := len(results)
	for _, t := range targets {
		res := map[string]any{"name": t.name, "outcome": OutcomeSuccess}
		if err := t.restic.Check(ctx, t.repo, verifyReadPercent); err != nil {
			res["outcome"], res["error"] = OutcomeFailure, err.Error()
			failed++
		} else if err := MarkRepoVerified(ctx, e.Pool.Pool, t.key, e.Now().UTC()); err != nil {
			e.Logger.Error("backup: mark verified", "repository", t.name, "error", err)
		}
		results = append(results, res)
	}
	outcome := OutcomeSuccess
	if failed > 0 {
		outcome = OutcomeFailure
	}
	detail := map[string]any{"repositories": results, "readDataPercent": verifyReadPercent}
	if err := RecordJobRun(ctx, e.Pool.Pool, "verify", started, e.Now().UTC(), outcome, detail); err != nil {
		e.Logger.Error("backup: record verify run", "error", err)
	}
	return outcome, detail
}

// Test proves a destination is usable by initialising (or opening) its
// repository from the worker, and records the result on the destination.
func (e *Executor) Test(ctx context.Context, id uuid.UUID) (string, map[string]any) {
	d, err := GetDestination(ctx, e.Pool, id)
	if err != nil {
		return OutcomeFailure, map[string]any{"error": err.Error()}
	}
	sess := e.openCloud(ctx)
	defer sess.Close(ctx)
	store := backupstore.New(db.Conn(ctx, e.Pool))
	fail := func(err error) (string, map[string]any) {
		sess.NoteFailure(ctx, d, err.Error())
		_ = store.RecordDestinationOutcome(ctx, backupstore.RecordDestinationOutcomeParams{Ok: false, ErrorText: err.Error(), ID: id})
		return OutcomeFailure, map[string]any{"name": d.Name, "error": err.Error()}
	}
	if err := sess.Usable(d); err != nil {
		return fail(err)
	}
	repo, err := d.Resolve(e.AllowedRoots)
	if err != nil {
		return fail(err)
	}
	restic := e.Restic
	if d.CloudAccountID != nil {
		restic = sess.Apply(restic)
	}
	local := LocalRepo(e.BackupDir)
	if err := EnsureRepo(ctx, e.Restic, local, nil); err != nil {
		return fail(err)
	}
	if err := EnsureRepo(ctx, restic, repo, &local); err != nil {
		return fail(err)
	}
	if bundle, err := LoadRecoveryBundle(ctx, e.Pool.Pool); err != nil {
		return fail(err)
	} else if bundle != nil {
		if err := PutRecoveryBundle(ctx, sess, d, repo, bundle); err != nil {
			return fail(err)
		}
	}
	if err := store.MarkDestinationInitialized(ctx, id); err != nil {
		return fail(err)
	}
	if err := store.RecordDestinationOutcome(ctx, backupstore.RecordDestinationOutcomeParams{Ok: true, ID: id}); err != nil {
		e.Logger.Error("backup: record destination outcome", "destination", d.Name, "error", err)
	}
	return OutcomeSuccess, map[string]any{"name": d.Name, "repository": repo.Location}
}

// ProcessNext reaps stale claims, claims one request, runs it and records the
// outcome. It reports whether a request was processed.
func (e *Executor) ProcessNext(ctx context.Context) bool {
	if n, err := ReapStaleRequests(ctx, e.Pool.Pool, e.Now().UTC()); err != nil {
		e.Logger.Error("backup: reap stale requests", "error", err)
	} else if n > 0 {
		e.Logger.Warn("backup: failed stale requests", "count", n)
	}
	req, ok, err := ClaimRequest(ctx, e.Pool.Pool, e.Now().UTC())
	if err != nil {
		e.Logger.Error("backup: claim request", "error", err)
		return false
	}
	if !ok {
		return false
	}
	e.Logger.Info("backup: request starting", "kind", req.Kind, "id", req.ID)

	var outcome string
	var detail any
	switch req.Kind {
	case RequestRun:
		rep, err := e.RunBackup(ctx)
		outcome, detail = rep.Outcome, rep
		if err != nil && !errors.Is(err, ErrLockHeld) {
			detail = map[string]any{"report": rep, "error": err.Error()}
		}
	case RequestTest:
		if req.DestinationID == nil {
			outcome, detail = OutcomeFailure, map[string]any{"error": "test request without destination"}
		} else {
			outcome, detail = e.Test(ctx, *req.DestinationID)
		}
	case RequestVerify:
		outcome, detail = e.Verify(ctx, req.DestinationID)
	default:
		outcome, detail = OutcomeFailure, map[string]any{"error": "unknown request kind " + req.Kind}
	}
	if err := FinishRequest(ctx, e.Pool.Pool, req.ID, outcome, detail, e.Now().UTC()); err != nil {
		e.Logger.Error("backup: finish request", "id", req.ID, "error", err)
	}
	e.Logger.Info("backup: request finished", "kind", req.Kind, "id", req.ID, "outcome", outcome)
	return true
}
