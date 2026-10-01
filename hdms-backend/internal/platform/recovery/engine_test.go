package recovery

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/backup"
)

var (
	testNow   = time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	testTaken = time.Date(2026, 9, 29, 2, 0, 0, 0, time.UTC)
)

const (
	scratchDB = "hdms_restore_20261001t090000"
	keptDB    = "hdms_before_20261001t090000"
)

// fakeOps records every call as a line, keeps a set of existing databases,
// and fails any call whose line starts with a key in fail. It also notes the
// step the state file held when each kind of call first ran, which is how
// the tests prove the state is persisted before the step's work.
type fakeOps struct {
	mu        sync.Mutex
	live      LiveState
	dbs       map[string]bool
	fail      map[string]error
	calls     []string
	statePath string
	stepAt    map[string]Step
	block     chan struct{}
}

func newFakeOps(statePath string) *fakeOps {
	return &fakeOps{
		live: LiveWorking, dbs: map[string]bool{"hdms": true}, fail: map[string]error{},
		statePath: statePath, stepAt: map[string]Step{},
	}
}

func (f *fakeOps) record(call string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
	name := strings.Fields(call)[0]
	if _, seen := f.stepAt[name]; !seen {
		if st, ok, _ := ReadState(f.statePath); ok {
			f.stepAt[name] = st.Step
		}
	}
	for prefix, err := range f.fail {
		if strings.HasPrefix(call, prefix) {
			return err
		}
	}
	return nil
}

func (f *fakeOps) set(name string, exists bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if exists {
		f.dbs[name] = true
	} else {
		delete(f.dbs, name)
	}
}

func (f *fakeOps) has(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.dbs[name]
}

func (f *fakeOps) callLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *fakeOps) LiveState(context.Context) LiveState {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.live
}

func (f *fakeOps) SafetyBackup(context.Context) (string, error) {
	return "safety1", f.record("SafetyBackup")
}

func (f *fakeOps) Exists(_ context.Context, name string) (bool, error) {
	return f.has(name), nil
}

func (f *fakeOps) CreateAndRestore(_ context.Context, name string, repo backup.Repo, snapshotID string) error {
	if f.block != nil {
		<-f.block
	}
	f.set(name, true) // the real one creates the database before restoring
	return f.record("CreateAndRestore " + name + " " + repo.Location + " " + snapshotID)
}

func (f *fakeOps) Prepare(_ context.Context, name string) error { return f.record("Prepare " + name) }

func (f *fakeOps) Validate(_ context.Context, name string) error { return f.record("Validate " + name) }

func (f *fakeOps) SetMaintenance(_ context.Context, name string, on bool) error {
	word := "off"
	if on {
		word = "on"
	}
	return f.record("SetMaintenance " + name + " " + word)
}

func (f *fakeOps) CopyForward(_ context.Context, from, to string, since time.Time) error {
	return f.record(fmt.Sprintf("CopyForward %s -> %s since %s", from, to, since.UTC().Format(time.RFC3339)))
}

func (f *fakeOps) Rename(_ context.Context, from, to string) error {
	if err := f.record("Rename " + from + " -> " + to); err != nil {
		return err
	}
	f.set(from, false)
	f.set(to, true)
	return nil
}

func (f *fakeOps) EnableConnections(context.Context, string) error { return nil }

func (f *fakeOps) Drop(_ context.Context, name string) error {
	f.set(name, false)
	return f.record("Drop " + name)
}

func (f *fakeOps) Record(_ context.Context, name string, st State) error {
	return f.record("Record " + name + " " + string(st.Kind))
}

func newTestEngine(t *testing.T) (*Engine, *fakeOps) {
	t.Helper()
	path := filepath.Join(t.TempDir(), StateFile)
	ops := newFakeOps(path)
	return &Engine{
		StatePath: path, LiveDB: "hdms", Ops: ops,
		Now:    func() time.Time { return testNow },
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Base:   context.Background(),
	}, ops
}

func testRequest() Request {
	return Request{
		Source:     Source{ID: "local", Kind: SourceLocal, Folder: "/backups"},
		SnapshotID: "snap1", SnapshotTakenAt: testTaken,
		UnlockIP: "10.0.0.5", UnlockedAt: testNow,
	}
}

func runRestore(t *testing.T, e *Engine) State {
	t.Helper()
	if _, err := e.Start(testRequest()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	e.Wait()
	st, _ := e.State()
	return st
}

var fullRestoreCalls = []string{
	"SafetyBackup",
	"CreateAndRestore " + scratchDB + " /backups/repo snap1",
	"Prepare " + scratchDB,
	"Validate " + scratchDB,
	"SetMaintenance hdms on",
	"CopyForward hdms -> " + scratchDB + " since 2026-09-29T02:00:00Z",
	"Rename hdms -> " + keptDB,
	"Rename " + scratchDB + " -> hdms",
	"SetMaintenance hdms off",
	"Record hdms restore",
}

func TestRestoreWithWorkingLiveRunsEveryStepInOrder(t *testing.T) {
	e, ops := newTestEngine(t)
	st := runRestore(t, e)

	if got := ops.callLog(); !slices.Equal(got, fullRestoreCalls) {
		t.Fatalf("calls:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(fullRestoreCalls, "\n"))
	}
	if st.Phase != PhaseCompleted || st.Error != "" || st.FinishedAt == nil {
		t.Fatalf("state = %+v, want completed", st)
	}
	if st.SafetySnapshotID != "safety1" || st.OutgoingDB != keptDB {
		t.Fatalf("safety %q kept %q", st.SafetySnapshotID, st.OutgoingDB)
	}
	if !ops.has("hdms") || !ops.has(keptDB) || ops.has(scratchDB) {
		t.Fatalf("databases afterwards = %v", ops.dbs)
	}
	wantStep := map[string]Step{
		"SafetyBackup": StepSafetyBackup, "CreateAndRestore": StepRestoreScratch,
		"Prepare": StepMigrateScratch, "Validate": StepValidate,
		"SetMaintenance": StepMaintenanceOn, "CopyForward": StepCopyForward,
		"Rename": StepSwap, "Record": StepRecord,
	}
	for call, want := range wantStep {
		if got := ops.stepAt[call]; got != want {
			t.Errorf("state file step when %s ran = %q, want %q (persist before the step)", call, got, want)
		}
	}
	onDisk, _, _ := ReadState(e.StatePath)
	if onDisk.Phase != PhaseCompleted {
		t.Fatalf("state file phase = %q, want completed", onDisk.Phase)
	}
}

func TestRestoreWithUnusableLiveSkipsTheSafetyNet(t *testing.T) {
	for _, live := range []LiveState{LiveDamaged, LiveEmpty} {
		t.Run(string(live), func(t *testing.T) {
			e, ops := newTestEngine(t)
			ops.live = live
			st := runRestore(t, e)
			want := []string{
				"CreateAndRestore " + scratchDB + " /backups/repo snap1",
				"Prepare " + scratchDB,
				"Validate " + scratchDB,
				"Rename hdms -> " + keptDB,
				"Rename " + scratchDB + " -> hdms",
				"Record hdms restore",
			}
			if got := ops.callLog(); !slices.Equal(got, want) {
				t.Fatalf("calls:\n%s", strings.Join(got, "\n"))
			}
			if st.Phase != PhaseCompleted || st.LiveState != live {
				t.Fatalf("state = %+v", st)
			}
		})
	}
}

func TestRestoreIntoAMissingDatabaseKeepsNothing(t *testing.T) {
	e, ops := newTestEngine(t)
	ops.live = LiveDamaged
	ops.set("hdms", false)
	st := runRestore(t, e)
	if slices.Contains(ops.callLog(), "Rename hdms -> "+keptDB) {
		t.Fatal("renamed a database that does not exist")
	}
	if st.Phase != PhaseCompleted || st.OutgoingDB != "" || !ops.has("hdms") {
		t.Fatalf("state = %+v, dbs = %v", st, ops.dbs)
	}
	if e.CanUndo(context.Background()) {
		t.Fatal("undo offered with nothing kept")
	}
}

func TestRestoreRefusesWhenTheServerIsDown(t *testing.T) {
	e, ops := newTestEngine(t)
	ops.live = LiveServerDown
	if _, err := e.Start(testRequest()); !errors.Is(err, ErrServerDown) {
		t.Fatalf("Start = %v, want ErrServerDown", err)
	}
	if _, ok, _ := ReadState(e.StatePath); ok {
		t.Fatal("a refused restore wrote a state file")
	}
}

func TestFailureAtEachStepUnwinds(t *testing.T) {
	boom := errors.New("boom")
	cases := []struct {
		fail     string
		err      error
		wantCode string
		wantTail []string
	}{
		{"SafetyBackup", boom, "safety_backup_failed", []string{"SafetyBackup"}},
		{"CreateAndRestore", boom, "restore_scratch_failed", []string{"CreateAndRestore " + scratchDB + " /backups/repo snap1", "Drop " + scratchDB}},
		{"Prepare", boom, "migrate_scratch_failed", []string{"Prepare " + scratchDB, "Drop " + scratchDB}},
		{"Validate", ErrRestoredWithoutAdmins, "no_admins", []string{"Validate " + scratchDB, "Drop " + scratchDB}},
		{"SetMaintenance hdms on", boom, "maintenance_on_failed", []string{"SetMaintenance hdms on", "SetMaintenance hdms off", "Drop " + scratchDB}},
		{"CopyForward", boom, "copy_forward_failed", []string{fullRestoreCalls[5], "SetMaintenance hdms off", "Drop " + scratchDB}},
		{"Rename " + scratchDB, boom, "swap_failed", []string{
			"Rename hdms -> " + keptDB, "Rename " + scratchDB + " -> hdms", "Rename " + keptDB + " -> hdms",
			"SetMaintenance hdms off", "Drop " + scratchDB,
		}},
	}
	for _, c := range cases {
		t.Run(c.fail, func(t *testing.T) {
			e, ops := newTestEngine(t)
			ops.fail[c.fail] = c.err
			st := runRestore(t, e)
			got := ops.callLog()
			if len(got) < len(c.wantTail) || !slices.Equal(got[len(got)-len(c.wantTail):], c.wantTail) {
				t.Fatalf("calls:\n%s\nwant to end with:\n%s", strings.Join(got, "\n"), strings.Join(c.wantTail, "\n"))
			}
			if st.Phase != PhaseFailed || st.Error != c.wantCode {
				t.Fatalf("phase %q error %q, want failed %q", st.Phase, st.Error, c.wantCode)
			}
			if !ops.has("hdms") || ops.has(scratchDB) || ops.has(keptDB) {
				t.Fatalf("databases after unwind = %v; live must be back, scratch and kept gone", ops.dbs)
			}
		})
	}
}

func TestFailureAfterTheSwapIsAWarning(t *testing.T) {
	e, ops := newTestEngine(t)
	ops.fail["SetMaintenance hdms off"] = errors.New("connection reset")
	st := runRestore(t, e)
	offs := 0
	for _, c := range ops.callLog() {
		if c == "SetMaintenance hdms off" {
			offs++
		}
	}
	if offs != 3 {
		t.Fatalf("maintenance_off tried %d times, want 3", offs)
	}
	if st.Phase != PhaseCompleted || st.Warning != "maintenance_off_failed" || st.Error != "" {
		t.Fatalf("state = %+v, want completed with a warning", st)
	}
	if !slices.Contains(ops.callLog(), "Record hdms restore") {
		t.Fatal("record must still run after a failed maintenance_off")
	}
}

func TestStartRefusesWhileRunning(t *testing.T) {
	e, ops := newTestEngine(t)
	ops.block = make(chan struct{})
	if _, err := e.Start(testRequest()); err != nil {
		t.Fatal(err)
	}
	if !e.Active() {
		t.Fatal("Active() = false during a restore")
	}
	if _, err := e.Start(testRequest()); !errors.Is(err, ErrRestoreActive) {
		t.Fatalf("second Start = %v, want ErrRestoreActive", err)
	}
	if _, err := e.Undo(UndoRequest{}); !errors.Is(err, ErrRestoreActive) {
		t.Fatalf("Undo during a restore = %v, want ErrRestoreActive", err)
	}
	close(ops.block)
	e.Wait()
	if e.Active() {
		t.Fatal("Active() = true after the restore ended")
	}
}

// runningAt is a restore interrupted at step, as the state file holds it.
func runningAt(step Step) State {
	return State{
		ID: "r1", Kind: KindRestore, Phase: PhaseRunning, Step: step,
		Steps:  plan(KindRestore, LiveWorking),
		Source: testRequest().Source, SnapshotID: "snap1", SnapshotTakenAt: testTaken,
		LiveState: LiveWorking, LiveDB: "hdms", IncomingDB: scratchDB, OutgoingDB: keptDB,
		CopySince: testTaken, StartedAt: testNow,
	}
}

func TestResumeAtEachStep(t *testing.T) {
	cases := []struct {
		name      string
		step      Step
		dbs       []string
		wantPhase Phase
		wantCalls []string
	}{
		{"before anything", StepSafetyBackup, []string{"hdms"}, PhaseFailed, nil},
		{"during the scratch restore", StepRestoreScratch, []string{"hdms", scratchDB}, PhaseFailed, []string{"Drop " + scratchDB}},
		{"during migrate", StepMigrateScratch, []string{"hdms", scratchDB}, PhaseFailed, []string{"Drop " + scratchDB}},
		{"during validate", StepValidate, []string{"hdms", scratchDB}, PhaseFailed, []string{"Drop " + scratchDB}},
		{"maintenance on", StepMaintenanceOn, []string{"hdms", scratchDB}, PhaseFailed, []string{"SetMaintenance hdms off", "Drop " + scratchDB}},
		{"copy forward", StepCopyForward, []string{"hdms", scratchDB}, PhaseFailed, []string{"SetMaintenance hdms off", "Drop " + scratchDB}},
		{"swap: nothing renamed", StepSwap, []string{"hdms", scratchDB}, PhaseFailed, []string{"SetMaintenance hdms off", "Drop " + scratchDB}},
		{"swap: live renamed away", StepSwap, []string{keptDB, scratchDB}, PhaseFailed, []string{"Rename " + keptDB + " -> hdms", "SetMaintenance hdms off", "Drop " + scratchDB}},
		{"swap: finished", StepSwap, []string{"hdms", keptDB}, PhaseCompleted, []string{"SetMaintenance hdms off", "Record hdms restore"}},
		{"maintenance off", StepMaintenanceOff, []string{"hdms", keptDB}, PhaseCompleted, []string{"SetMaintenance hdms off", "Record hdms restore"}},
		{"record", StepRecord, []string{"hdms", keptDB}, PhaseCompleted, []string{"Record hdms restore"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e, ops := newTestEngine(t)
			ops.dbs = map[string]bool{}
			for _, n := range c.dbs {
				ops.dbs[n] = true
			}
			if err := WriteState(e.StatePath, runningAt(c.step)); err != nil {
				t.Fatal(err)
			}
			if err := e.Resume(context.Background()); err != nil {
				t.Fatalf("Resume: %v", err)
			}
			e.Wait()
			st, _, _ := ReadState(e.StatePath)
			if st.Phase != c.wantPhase {
				t.Fatalf("phase = %q, want %q (state %+v)", st.Phase, c.wantPhase, st)
			}
			if c.wantPhase == PhaseFailed && st.Error != "interrupted" {
				t.Fatalf("error = %q, want interrupted", st.Error)
			}
			if got := ops.callLog(); !slices.Equal(got, c.wantCalls) {
				t.Fatalf("calls:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(c.wantCalls, "\n"))
			}
			if !ops.has("hdms") || ops.has(scratchDB) {
				t.Fatalf("databases = %v; live must exist and scratch must be gone", ops.dbs)
			}
		})
	}
}

func TestResumeWithAFinishedStateDoesNothing(t *testing.T) {
	e, ops := newTestEngine(t)
	done := runningAt(StepRecord)
	done.Phase = PhaseCompleted
	if err := WriteState(e.StatePath, done); err != nil {
		t.Fatal(err)
	}
	if err := e.Resume(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(ops.callLog()) != 0 || e.Active() {
		t.Fatalf("calls %v active %v", ops.callLog(), e.Active())
	}
	if st, ok := e.State(); !ok || st.Phase != PhaseCompleted {
		t.Fatalf("State() = %+v %v; Resume must load the finished state", st, ok)
	}
}

func TestResumeSetsACorruptStateFileAside(t *testing.T) {
	e, _ := newTestEngine(t)
	if err := WriteState(e.StatePath, State{}); err != nil {
		t.Fatal(err)
	}
	if err := writeRaw(e.StatePath, "{broken"); err != nil {
		t.Fatal(err)
	}
	if err := e.Resume(context.Background()); err != nil {
		t.Fatalf("Resume = %v; a corrupt file must not stop the worker", err)
	}
	if _, ok, err := ReadState(e.StatePath); ok || err != nil {
		t.Fatalf("state file still there: ok=%v err=%v", ok, err)
	}
	matches, _ := filepath.Glob(e.StatePath + ".corrupt-*")
	if len(matches) != 1 {
		t.Fatalf("corrupt file kept as %v, want one .corrupt-* copy", matches)
	}
}

func TestUndoSwapsBackAndCopiesForward(t *testing.T) {
	e, ops := newTestEngine(t)
	runRestore(t, e)
	if !e.CanUndo(context.Background()) {
		t.Fatal("CanUndo = false after a restore over a working database")
	}
	before := len(ops.callLog())
	if _, err := e.Undo(UndoRequest{UnlockIP: "10.0.0.5", UnlockedAt: testNow}); err != nil {
		t.Fatal(err)
	}
	e.Wait()
	rolled := "hdms_rolledback_20261001t090000"
	want := []string{
		"SetMaintenance hdms on",
		"CopyForward hdms -> " + keptDB + " since 2026-10-01T09:00:00Z",
		"Rename hdms -> " + rolled,
		"Rename " + keptDB + " -> hdms",
		"SetMaintenance hdms off",
		"Record hdms undo",
	}
	if got := ops.callLog()[before:]; !slices.Equal(got, want) {
		t.Fatalf("undo calls:\n%s", strings.Join(got, "\n"))
	}
	st, _ := e.State()
	if st.Kind != KindUndo || st.Phase != PhaseCompleted || st.UndoOf == "" {
		t.Fatalf("undo state = %+v", st)
	}
	if _, err := e.Undo(UndoRequest{}); !errors.Is(err, ErrNothingToUndo) {
		t.Fatalf("second Undo = %v, want ErrNothingToUndo", err)
	}
}

func TestUndoIsRefusedWhenTheReplacedDatabaseWasNotWorking(t *testing.T) {
	e, ops := newTestEngine(t)
	ops.live = LiveDamaged
	runRestore(t, e)
	if e.CanUndo(context.Background()) {
		t.Fatal("undo offered back to a damaged database")
	}
	if _, err := e.Undo(UndoRequest{}); !errors.Is(err, ErrNothingToUndo) {
		t.Fatalf("Undo = %v, want ErrNothingToUndo", err)
	}
}

func TestUndoIsRefusedWhenTheKeptDatabaseIsGone(t *testing.T) {
	e, ops := newTestEngine(t)
	runRestore(t, e)
	ops.set(keptDB, false) // IT dropped it per the runbook
	if _, err := e.Undo(UndoRequest{}); !errors.Is(err, ErrNothingToUndo) {
		t.Fatalf("Undo = %v, want ErrNothingToUndo", err)
	}
}

func TestViewHidesDatabaseNames(t *testing.T) {
	e, _ := newTestEngine(t)
	if e.View(context.Background()) != nil {
		t.Fatal("View before any restore must be nil")
	}
	runRestore(t, e)
	v := e.View(context.Background())
	if v == nil || v.Phase != PhaseCompleted || !v.CanUndo || v.SourceKind != SourceLocal {
		t.Fatalf("view = %+v", v)
	}
}

func TestLongDatabaseNamesAreRefused(t *testing.T) {
	e, _ := newTestEngine(t)
	e.LiveDB = strings.Repeat("h", 50)
	if _, err := e.Start(testRequest()); !errors.Is(err, ErrNameTooLong) {
		t.Fatalf("Start = %v, want ErrNameTooLong", err)
	}
}
