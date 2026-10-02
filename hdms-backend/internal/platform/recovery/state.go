// Package recovery restores HDMS from a backup without the API and without a
// working database: the engine behind /recovery on the worker. Its progress
// lives in a state file beside the backups, because the database may be the
// thing that is broken.
package recovery

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/backup"
)

// StateFile sits in HDMS_BACKUP_DIR, on the backup volume, not in the
// database.
const StateFile = "restore-state.json"

var ErrStateCorrupt = errors.New("recovery: restore state file is unreadable")

type Step string

const (
	StepSafetyBackup   Step = "safety_backup"
	StepRestoreScratch Step = "restore_scratch"
	StepMigrateScratch Step = "migrate_scratch"
	StepValidate       Step = "validate"
	StepMaintenanceOn  Step = "maintenance_on"
	StepCopyForward    Step = "copy_forward"
	StepSwap           Step = "swap"
	StepMaintenanceOff Step = "maintenance_off"
	StepRecord         Step = "record"
)

type Phase string

const (
	PhaseRunning   Phase = "running"
	PhaseCompleted Phase = "completed"
	PhaseFailed    Phase = "failed"
)

type Kind string

const (
	KindRestore Kind = "restore"
	KindUndo    Kind = "undo"
)

// LiveState is what the engine finds in the live database before it starts.
type LiveState string

const (
	// LiveWorking: schema current and at least one admin account.
	LiveWorking LiveState = "working"
	// LiveEmpty: schema current but no admin — a freshly installed server.
	LiveEmpty LiveState = "empty"
	// LiveDamaged: missing, unreachable, or schema not current.
	LiveDamaged LiveState = "damaged"
	// LiveServerDown: Postgres itself does not answer.
	LiveServerDown LiveState = "server_down"
)

const (
	SourceLocal       = "local"
	SourceFolder      = "folder"
	SourceDestination = "destination"
)

// Source is a folder holding a repo subfolder and hdms-recovery.bin.
type Source struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Name   string `json:"name,omitempty"`
	Folder string `json:"folder"`
	HasKey bool   `json:"hasRecoveryKey"`
}

func (s Source) Repo() backup.Repo {
	return backup.Repo{Location: filepath.Join(s.Folder, "repo")}
}

// State is one restore or undo. IncomingDB becomes live; the live database
// is renamed to OutgoingDB. For a restore, IncomingDB is the scratch database
// and OutgoingDB the kept previous one; for an undo, the other way round.
type State struct {
	ID               string    `json:"id"`
	Kind             Kind      `json:"kind"`
	Phase            Phase     `json:"phase"`
	Step             Step      `json:"step"`
	Steps            []Step    `json:"steps"`
	Source           Source    `json:"source"`
	SnapshotID       string    `json:"snapshotId,omitempty"`
	SnapshotTakenAt  time.Time `json:"snapshotTakenAt"`
	LiveState        LiveState `json:"liveState"`
	LiveDB           string    `json:"liveDb"`
	IncomingDB       string    `json:"incomingDb"`
	OutgoingDB       string    `json:"outgoingDb"`
	CopySince        time.Time `json:"copySince"`
	SafetySnapshotID string    `json:"safetySnapshotId,omitempty"`
	UndoOf           string    `json:"undoOf,omitempty"`
	UnlockIP         string    `json:"unlockIp,omitempty"`
	UnlockedAt       time.Time `json:"unlockedAt"`
	// RequestedBy is who asked: "recovery-key" for the recovery page,
	// "admin:<id>" for the console. Empty in state files from before it
	// existed, which were all the recovery page's.
	RequestedBy string     `json:"requestedBy,omitempty"`
	StartedAt   time.Time  `json:"startedAt"`
	FinishedAt  *time.Time `json:"finishedAt,omitempty"`
	// DiscardedAt: the kept database was dropped from the console; undo
	// is no longer possible.
	DiscardedAt *time.Time `json:"discardedAt,omitempty"`
	// Error is a stable code: "<step>_failed", "interrupted" or "no_admins".
	Error string `json:"error,omitempty"`
	// Warning is a step after the swap that failed; the restore stands.
	Warning string `json:"warning,omitempty"`
}

// View is what the recovery page sees: no database names, no IP.
type View struct {
	Kind            Kind       `json:"kind"`
	Phase           Phase      `json:"phase"`
	Step            Step       `json:"step"`
	Steps           []Step     `json:"steps"`
	SourceKind      string     `json:"sourceKind"`
	SourceName      string     `json:"sourceName,omitempty"`
	SnapshotTakenAt time.Time  `json:"snapshotTakenAt"`
	StartedAt       time.Time  `json:"startedAt"`
	FinishedAt      *time.Time `json:"finishedAt,omitempty"`
	Error           string     `json:"error,omitempty"`
	Warning         string     `json:"warning,omitempty"`
	CanUndo         bool       `json:"canUndo"`
	CanDiscard      bool       `json:"canDiscard"`
	DiscardedAt     *time.Time `json:"discardedAt,omitempty"`
}

func viewOf(st State) View {
	return View{
		Kind: st.Kind, Phase: st.Phase, Step: st.Step, Steps: st.Steps,
		SourceKind: st.Source.Kind, SourceName: st.Source.Name,
		SnapshotTakenAt: st.SnapshotTakenAt, StartedAt: st.StartedAt, FinishedAt: st.FinishedAt,
		DiscardedAt: st.DiscardedAt,
		Error:       st.Error, Warning: st.Warning,
	}
}

// ReadState returns the state file's contents and whether it exists.
func ReadState(path string) (State, bool, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- path is HDMS_BACKUP_DIR configuration
	if errors.Is(err, fs.ErrNotExist) {
		return State{}, false, nil
	}
	if err != nil {
		return State{}, false, fmt.Errorf("recovery: read state: %w", err)
	}
	var st State
	if err := json.Unmarshal(raw, &st); err != nil {
		return State{}, false, fmt.Errorf("%w: %v", ErrStateCorrupt, err)
	}
	return st, true, nil
}

// WriteState replaces the state file atomically and durably: a crash leaves
// the old state or the new one, never half of one.
func WriteState(path string, st State) error {
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".restore-state-*")
	if err != nil {
		return fmt.Errorf("recovery: write state: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("recovery: write state: %w", err)
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("recovery: write state: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("recovery: write state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("recovery: write state: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("recovery: write state: %w", err)
	}
	return nil
}
