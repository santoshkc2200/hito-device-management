package recovery

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/hito-hospital/hdms/internal/platform/backup"
)

var (
	ErrRestoreActive = errors.New("recovery: a restore is already running")
	ErrNothingToUndo = errors.New("recovery: there is no restore to undo")
	ErrServerDown    = errors.New("recovery: the database server is not running")
	ErrNameTooLong   = errors.New("recovery: database name too long for a restore suffix")
	// ErrRestoredWithoutAdmins: the snapshot has no admin account, so nobody
	// could sign in after the restore.
	ErrRestoredWithoutAdmins = errors.New("recovery: the backup holds no administrator account")
	errInterrupted           = errors.New("recovery: interrupted by a worker restart")
	ErrNothingToDiscard      = errors.New("recovery: there is no kept database to discard")
)

// maxIdentifier is Postgres's identifier limit in bytes.
const maxIdentifier = 63

// RequesterRecoveryKey names restores asked for through /recovery.
const RequesterRecoveryKey = "recovery-key"

// Ops are the database and repository operations a restore is made of. The
// engine decides their order, persists progress and unwinds; PGOps does the
// work against Postgres and restic.
type Ops interface {
	LiveState(ctx context.Context) LiveState
	SafetyBackup(ctx context.Context) (string, error)
	Exists(ctx context.Context, name string) (bool, error)
	CreateAndRestore(ctx context.Context, name string, repo backup.Repo, snapshotID string) error
	Prepare(ctx context.Context, name string) error
	Validate(ctx context.Context, name string) error
	SetMaintenance(ctx context.Context, name string, on bool) error
	CopyForward(ctx context.Context, from, to string, since time.Time) error
	Rename(ctx context.Context, from, to string) error
	EnableConnections(ctx context.Context, name string) error
	Drop(ctx context.Context, name string) error
	Record(ctx context.Context, name string, st State) error
}

// Request is a restore the recovery page asked for.
type Request struct {
	Source          Source
	SnapshotID      string
	SnapshotTakenAt time.Time
	UnlockIP        string
	UnlockedAt      time.Time
	RequestedBy     string
}

type UndoRequest struct {
	UnlockIP    string
	UnlockedAt  time.Time
	RequestedBy string
}

// Engine runs one restore or undo at a time in the background and keeps its
// progress in the state file at StatePath.
type Engine struct {
	StatePath string
	LiveDB    string
	Ops       Ops
	Now       func() time.Time
	Logger    *slog.Logger
	// Settle is the wait after maintenance goes on, longer than the API's
	// two-second cache of the flag, before anything is copied.
	Settle time.Duration
	// RetryDelay separates the three maintenance_off attempts.
	RetryDelay time.Duration
	// Finished runs after every restore or undo, whatever the outcome; the
	// worker retries its own database start from it.
	Finished func()
	// Base is the context runs use. It ends at SIGTERM, which leaves the
	// state file as it is for Resume on the next start.
	Base context.Context

	mu      sync.Mutex
	st      State
	has     bool
	running bool
	done    chan struct{}
}

func stamp(t time.Time) string { return t.UTC().Format("20060102t150405") }

// plan is the step list. Without a working live database there is nothing
// to save or freeze: no safety backup, maintenance or copy-forward.
func plan(kind Kind, live LiveState) []Step {
	full := live == LiveWorking
	switch {
	case kind == KindRestore && full:
		return []Step{StepSafetyBackup, StepRestoreScratch, StepMigrateScratch, StepValidate,
			StepMaintenanceOn, StepCopyForward, StepSwap, StepMaintenanceOff, StepRecord}
	case kind == KindRestore:
		return []Step{StepRestoreScratch, StepMigrateScratch, StepValidate, StepSwap, StepRecord}
	case full:
		return []Step{StepMaintenanceOn, StepCopyForward, StepSwap, StepMaintenanceOff, StepRecord}
	default:
		return []Step{StepSwap, StepRecord}
	}
}

func (e *Engine) State() (State, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.st, e.has
}

// Active reports a run in progress, or a state file that names one not yet
// resumed. While it is true the worker leaves the database alone.
func (e *Engine) Active() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.activeLocked()
}

func (e *Engine) activeLocked() bool {
	return e.running || (e.has && e.st.Phase == PhaseRunning)
}

// Wait blocks until the current run, if any, has ended.
func (e *Engine) Wait() {
	e.mu.Lock()
	d := e.done
	e.mu.Unlock()
	if d != nil {
		<-d
	}
}

func (e *Engine) Start(req Request) (State, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.activeLocked() {
		return State{}, ErrRestoreActive
	}
	live := e.Ops.LiveState(e.Base)
	if live == LiveServerDown {
		return State{}, ErrServerDown
	}
	now := e.Now().UTC()
	ts := stamp(now)
	return e.beginLocked(State{
		ID: uuid.NewString(), Kind: KindRestore, Phase: PhaseRunning, Steps: plan(KindRestore, live),
		Source: req.Source, SnapshotID: req.SnapshotID, SnapshotTakenAt: req.SnapshotTakenAt,
		LiveState: live, LiveDB: e.LiveDB,
		IncomingDB: e.LiveDB + "_restore_" + ts, OutgoingDB: e.LiveDB + "_before_" + ts,
		CopySince: req.SnapshotTakenAt, UnlockIP: req.UnlockIP, UnlockedAt: req.UnlockedAt,
		RequestedBy: req.RequestedBy, StartedAt: now,
	})
}

func (e *Engine) Undo(req UndoRequest) (State, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.activeLocked() {
		return State{}, ErrRestoreActive
	}
	if !e.canUndoLocked(e.Base) {
		return State{}, ErrNothingToUndo
	}
	prev := e.st
	live := e.Ops.LiveState(e.Base)
	if live == LiveServerDown {
		return State{}, ErrServerDown
	}
	now := e.Now().UTC()
	// Everything the restored database recorded since the restore began,
	// including the unlock that led to it, goes back with the undo.
	since := prev.StartedAt
	if !prev.UnlockedAt.IsZero() && prev.UnlockedAt.Before(since) {
		since = prev.UnlockedAt
	}
	return e.beginLocked(State{
		ID: uuid.NewString(), Kind: KindUndo, Phase: PhaseRunning, Steps: plan(KindUndo, live),
		Source: prev.Source, SnapshotID: prev.SnapshotID, SnapshotTakenAt: prev.SnapshotTakenAt,
		LiveState: live, LiveDB: e.LiveDB,
		IncomingDB: prev.OutgoingDB, OutgoingDB: e.LiveDB + "_rolledback_" + stamp(now),
		CopySince: since, UndoOf: prev.ID,
		UnlockIP: req.UnlockIP, UnlockedAt: req.UnlockedAt,
		RequestedBy: req.RequestedBy, StartedAt: now,
	})
}

// CanUndo: the last run was a completed restore over a working database
// whose kept copy still exists.
func (e *Engine) CanUndo(ctx context.Context) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.canUndoLocked(ctx)
}

func (e *Engine) canUndoLocked(ctx context.Context) bool {
	st := e.st
	if !e.has || e.running || st.Kind != KindRestore || st.Phase != PhaseCompleted ||
		st.OutgoingDB == "" || st.LiveState != LiveWorking {
		return false
	}
	ok, err := e.Ops.Exists(ctx, st.OutgoingDB)
	return err == nil && ok
}

// canDiscardLocked: the last run completed and the database it kept still
// exists. The live name is never discardable, whatever the state file says.
func (e *Engine) canDiscardLocked(ctx context.Context) bool {
	st := e.st
	if !e.has || e.running || st.Phase != PhaseCompleted || st.OutgoingDB == "" || st.OutgoingDB == e.LiveDB {
		return false
	}
	ok, err := e.Ops.Exists(ctx, st.OutgoingDB)
	return err == nil && ok
}

// Discard drops the database the last restore or undo kept, which ends
// the chance to undo it. The state file then forgets the name.
func (e *Engine) Discard(by string) (State, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.activeLocked() {
		return State{}, ErrRestoreActive
	}
	if !e.canDiscardLocked(e.Base) {
		return State{}, ErrNothingToDiscard
	}
	st := e.st
	if err := e.Ops.Drop(e.Base, st.OutgoingDB); err != nil {
		return State{}, err
	}
	e.Logger.Info("recovery: kept database discarded", "database", st.OutgoingDB, "by", by)
	now := e.Now().UTC()
	st.OutgoingDB, st.DiscardedAt = "", &now
	e.st = st
	if err := WriteState(e.StatePath, st); err != nil {
		return st, err
	}
	return st, nil
}

// View is the recovery page's picture of the last run, or nil.
func (e *Engine) View(ctx context.Context) *View {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.has {
		return nil
	}
	v := viewOf(e.st)
	v.CanUndo = e.canUndoLocked(ctx)
	v.CanDiscard = e.canDiscardLocked(ctx)
	return &v
}

func (e *Engine) beginLocked(st State) (State, error) {
	if len(st.IncomingDB) > maxIdentifier || len(st.OutgoingDB) > maxIdentifier {
		return State{}, ErrNameTooLong
	}
	st.Step = st.Steps[0]
	if err := WriteState(e.StatePath, st); err != nil {
		return State{}, err
	}
	e.st, e.has = st, true
	e.launchLocked()
	return st, nil
}

func (e *Engine) launchLocked() {
	e.running = true
	done := make(chan struct{})
	e.done = done
	go func() {
		defer func() {
			e.mu.Lock()
			e.running = false
			e.mu.Unlock()
			close(done)
			if e.Finished != nil {
				e.Finished()
			}
		}()
		e.runForward(e.Base)
	}()
}

// save writes the state file and updates the in-memory copy, which is kept
// even when the write fails so the page still shows what happened.
func (e *Engine) save(st State) error {
	err := WriteState(e.StatePath, st)
	e.mu.Lock()
	e.st, e.has = st, true
	e.mu.Unlock()
	return err
}

func (e *Engine) runForward(ctx context.Context) {
	st, _ := e.State()
	for i := slices.Index(st.Steps, st.Step); i < len(st.Steps); i++ {
		st.Step = st.Steps[i]
		if err := e.save(st); err != nil {
			e.Logger.Error("recovery: cannot write the restore state; stopping", "step", st.Step, "error", err)
			if afterSwap(st.Step) {
				continue
			}
			e.unwind(ctx, &st, err)
			return
		}
		e.Logger.Info("recovery: step", "kind", st.Kind, "step", st.Step)
		err := e.do(ctx, &st)
		if err == nil {
			continue
		}
		if ctx.Err() != nil {
			e.Logger.Warn("recovery: stopped mid-step; the next start resumes or unwinds", "step", st.Step)
			return
		}
		if afterSwap(st.Step) {
			e.Logger.Error("recovery: step failed after the swap; the restore stands", "step", st.Step, "error", err)
			st.Warning = string(st.Step) + "_failed"
			continue
		}
		e.Logger.Error("recovery: step failed; undoing what was done", "step", st.Step, "error", err)
		e.unwind(ctx, &st, err)
		return
	}
	now := e.Now().UTC()
	st.Phase, st.FinishedAt = PhaseCompleted, &now
	if err := e.save(st); err != nil {
		e.Logger.Error("recovery: write the final restore state", "error", err)
	}
	e.Logger.Info("recovery: finished", "kind", st.Kind, "warning", st.Warning)
}

func afterSwap(s Step) bool { return s == StepMaintenanceOff || s == StepRecord }

func (e *Engine) do(ctx context.Context, st *State) error {
	switch st.Step {
	case StepSafetyBackup:
		id, err := e.Ops.SafetyBackup(ctx)
		st.SafetySnapshotID = id
		return err
	case StepRestoreScratch:
		return e.Ops.CreateAndRestore(ctx, st.IncomingDB, st.Source.Repo(), st.SnapshotID)
	case StepMigrateScratch:
		return e.Ops.Prepare(ctx, st.IncomingDB)
	case StepValidate:
		return e.Ops.Validate(ctx, st.IncomingDB)
	case StepMaintenanceOn:
		if err := e.Ops.SetMaintenance(ctx, st.LiveDB, true); err != nil {
			return err
		}
		return sleep(ctx, e.Settle)
	case StepCopyForward:
		return e.Ops.CopyForward(ctx, st.LiveDB, st.IncomingDB, st.CopySince)
	case StepSwap:
		return e.swap(ctx, st)
	case StepMaintenanceOff:
		var err error
		for attempt := 0; attempt < 3; attempt++ {
			if err = e.Ops.SetMaintenance(ctx, st.LiveDB, false); err == nil {
				return nil
			}
			if serr := sleep(ctx, e.RetryDelay); serr != nil {
				return serr
			}
		}
		return err
	case StepRecord:
		return e.Ops.Record(ctx, st.LiveDB, *st)
	}
	return fmt.Errorf("recovery: unknown step %q", st.Step)
}

// swap renames live away and incoming into its place. If the second rename
// fails the first is reversed, so live is never left missing on purpose.
func (e *Engine) swap(ctx context.Context, st *State) error {
	liveExists, err := e.Ops.Exists(ctx, st.LiveDB)
	if err != nil {
		return err
	}
	if !liveExists {
		st.OutgoingDB = ""
		return e.Ops.Rename(ctx, st.IncomingDB, st.LiveDB)
	}
	if err := e.Ops.Rename(ctx, st.LiveDB, st.OutgoingDB); err != nil {
		return err
	}
	if err := e.Ops.Rename(ctx, st.IncomingDB, st.LiveDB); err != nil {
		if back := e.Ops.Rename(ctx, st.OutgoingDB, st.LiveDB); back != nil {
			e.Logger.Error("recovery: could not put the live database back; IT must rename it (runbook)",
				"database", st.OutgoingDB, "error", back)
		}
		return err
	}
	return nil
}

// unwind undoes what the failed step and those before it left behind. A
// restore's scratch database is dropped; an undo's incoming database is the
// kept original and is never dropped.
func (e *Engine) unwind(ctx context.Context, st *State, cause error) {
	if reached(*st, StepMaintenanceOn) {
		if err := e.Ops.SetMaintenance(ctx, st.LiveDB, false); err != nil {
			e.Logger.Error("recovery: switch maintenance off while unwinding", "error", err)
		}
	}
	if st.Kind == KindRestore && reached(*st, StepRestoreScratch) {
		if err := e.Ops.Drop(ctx, st.IncomingDB); err != nil {
			e.Logger.Error("recovery: drop the scratch database while unwinding", "database", st.IncomingDB, "error", err)
		}
	}
	now := e.Now().UTC()
	st.Phase, st.FinishedAt, st.Error = PhaseFailed, &now, errorCode(st.Step, cause)
	if err := e.save(*st); err != nil {
		e.Logger.Error("recovery: write the failed restore state", "error", err)
	}
}

// reached: s is in the plan and at or before the current step.
func reached(st State, s Step) bool {
	i := slices.Index(st.Steps, s)
	return i >= 0 && i <= slices.Index(st.Steps, st.Step)
}

func errorCode(step Step, cause error) string {
	switch {
	case errors.Is(cause, errInterrupted):
		return "interrupted"
	case errors.Is(cause, ErrRestoredWithoutAdmins):
		return "no_admins"
	}
	return string(step) + "_failed"
}

// Resume loads the state file at worker start. A run interrupted before its
// swap finished is unwound; one interrupted after it is carried to the end.
// An error (the database server not answering yet) is retried by the caller.
func (e *Engine) Resume(ctx context.Context) error {
	e.mu.Lock()
	running := e.running
	e.mu.Unlock()
	if running {
		return nil
	}
	st, ok, err := ReadState(e.StatePath)
	if errors.Is(err, ErrStateCorrupt) {
		aside := e.StatePath + ".corrupt-" + stamp(e.Now())
		e.Logger.Error("recovery: restore state file is unreadable; set aside", "kept_as", aside, "error", err)
		return os.Rename(e.StatePath, aside)
	}
	if err != nil {
		return err
	}
	e.mu.Lock()
	e.st, e.has = st, ok
	e.mu.Unlock()
	if !ok || st.Phase != PhaseRunning {
		return nil
	}
	e.Logger.Warn("recovery: found an interrupted restore", "kind", st.Kind, "step", st.Step)

	// A crash between disabling connections and renaming leaves a database
	// nobody can connect to.
	for _, name := range []string{st.LiveDB, st.IncomingDB, st.OutgoingDB} {
		if name == "" {
			continue
		}
		exists, err := e.Ops.Exists(ctx, name)
		if err != nil {
			return err
		}
		if exists {
			if err := e.Ops.EnableConnections(ctx, name); err != nil {
				return err
			}
		}
	}

	at, swapAt := slices.Index(st.Steps, st.Step), slices.Index(st.Steps, StepSwap)
	if at == swapAt {
		done, err := e.swapCompleted(ctx, &st)
		if err != nil {
			return err
		}
		if done {
			at++
			st.Step = st.Steps[at]
		}
	}
	if at <= swapAt {
		e.unwind(ctx, &st, errInterrupted)
		return nil
	}
	e.mu.Lock()
	e.st = st
	e.launchLocked()
	e.mu.Unlock()
	return nil
}

// swapCompleted works out how far an interrupted swap got. It completes
// nothing: a half swap is put back, and the caller then unwinds.
func (e *Engine) swapCompleted(ctx context.Context, st *State) (bool, error) {
	exists := func(n string) (bool, error) {
		if n == "" {
			return false, nil
		}
		return e.Ops.Exists(ctx, n)
	}
	live, err := exists(st.LiveDB)
	if err != nil {
		return false, err
	}
	incoming, err := exists(st.IncomingDB)
	if err != nil {
		return false, err
	}
	outgoing, err := exists(st.OutgoingDB)
	if err != nil {
		return false, err
	}
	switch {
	case live && !incoming:
		if !outgoing {
			st.OutgoingDB = ""
		}
		return true, nil
	case !live && outgoing:
		return false, e.Ops.Rename(ctx, st.OutgoingDB, st.LiveDB)
	}
	return false, nil
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}
