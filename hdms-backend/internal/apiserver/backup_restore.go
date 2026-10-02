package apiserver

import (
	"errors"
	"net/http"

	"github.com/hito-hospital/hdms/internal/platform/backup"
	"github.com/hito-hospital/hdms/internal/platform/httpx"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
	"github.com/hito-hospital/hdms/internal/platform/recovery"
)

const restoreConfirmWord = "RESTORE"

// writeRestoreError maps the worker's restore errors to problems; anything
// else goes through writeBackupError.
func (s *Server) writeRestoreError(w http.ResponseWriter, r *http.Request, err error) {
	problem := func(typ, title string, status int) {
		httpx.WriteProblem(w, r, httpx.NewProblem(typ, title, status))
	}
	switch {
	case errors.Is(err, recovery.ErrRestoreActive):
		problem("restore-running", "A restore is already running", http.StatusConflict)
	case errors.Is(err, recovery.ErrNothingToUndo):
		problem("nothing-to-undo", "There is no restore to roll back", http.StatusConflict)
	case errors.Is(err, recovery.ErrNothingToDiscard):
		problem("nothing-to-discard", "There is no kept copy to discard", http.StatusConflict)
	case errors.Is(err, recovery.ErrServerDown):
		problem("database-server-down", "The database server is not running", http.StatusConflict)
	case errors.Is(err, recovery.ErrSourceUnsupported):
		problem("restore-source-unsupported", "Restoring from this location is not available yet", http.StatusUnprocessableEntity)
	case errors.Is(err, recovery.ErrRepositoryUnreadable):
		problem("repository-unreadable", "The backup location cannot be read", http.StatusBadGateway)
	case errors.Is(err, recovery.ErrSourceNotFound), errors.Is(err, recovery.ErrSnapshotNotFound):
		problem("not-found", "Not found", http.StatusNotFound)
	default:
		s.writeBackupError(w, r, err)
	}
}

func mapRestoreView(v recovery.View) gen.BackupRestore {
	steps := make([]string, len(v.Steps))
	for i, st := range v.Steps {
		steps[i] = string(st)
	}
	out := gen.BackupRestore{
		Kind:            gen.BackupRestoreKind(v.Kind),
		Phase:           gen.BackupRestorePhase(v.Phase),
		Step:            string(v.Step),
		Steps:           steps,
		SourceKind:      gen.BackupRestoreSourceKind(v.SourceKind),
		SnapshotTakenAt: v.SnapshotTakenAt,
		StartedAt:       v.StartedAt,
		FinishedAt:      v.FinishedAt,
		CanUndo:         v.CanUndo,
		CanDiscard:      v.CanDiscard,
		DiscardedAt:     v.DiscardedAt,
	}
	if v.SourceName != "" {
		out.SourceName = strPtr(v.SourceName)
	}
	if v.Error != "" {
		out.Error = strPtr(v.Error)
	}
	if v.Warning != "" {
		out.Warning = strPtr(v.Warning)
	}
	return out
}

// restoreStatus pairs the worker's view with the maintenance flag, which the
// API reads itself so the console can end a stuck maintenance mode even
// when the worker does not answer.
func (s *Server) restoreStatus(r *http.Request, view *recovery.View, workerUp bool) (gen.BackupRestoreStatus, error) {
	m, err := backup.GetMaintenance(r.Context(), s.pool.Pool)
	if err != nil {
		return gen.BackupRestoreStatus{}, err
	}
	out := gen.BackupRestoreStatus{Maintenance: m.On, WorkerAvailable: workerUp}
	if view != nil {
		v := mapRestoreView(*view)
		out.Restore = &v
	}
	return out, nil
}

func (s *Server) writeRestoreStatus(w http.ResponseWriter, r *http.Request, status int, view *recovery.View) {
	out, err := s.restoreStatus(r, view, true)
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	writeJSON(w, status, out)
}

func (s *Server) GetBackupRestore(w http.ResponseWriter, r *http.Request) {
	view, err := s.backupCfg.Restore.State(r.Context())
	workerUp := !errors.Is(err, backup.ErrWorkerUnavailable)
	if err != nil && workerUp {
		s.writeRestoreError(w, r, err)
		return
	}
	out, err := s.restoreStatus(r, view, workerUp)
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func confirmed(w http.ResponseWriter, r *http.Request, word string) bool {
	if word == restoreConfirmWord {
		return true
	}
	httpx.WriteProblem(w, r, httpx.NewProblem("confirmation-required", "Type RESTORE to confirm", http.StatusUnprocessableEntity))
	return false
}

func (s *Server) StartBackupRestore(w http.ResponseWriter, r *http.Request) {
	body, ok := decodeJSON[gen.BackupRestoreInput](w, r)
	if !ok || !confirmed(w, r, string(body.Confirmation)) || !s.reauthenticate(w, r, body.Password, body.TotpCode) {
		return
	}
	view, err := s.backupCfg.Restore.Start(r.Context(), body.Repo, body.SnapshotId, actorFrom(r))
	if err != nil {
		s.writeRestoreError(w, r, err)
		return
	}
	s.recordBackupAudit(r, "backup.restore.requested", "backup:restore", map[string]any{"repo": body.Repo, "snapshotId": body.SnapshotId})
	s.writeRestoreStatus(w, r, http.StatusAccepted, view)
}

func (s *Server) UndoBackupRestore(w http.ResponseWriter, r *http.Request) {
	body, ok := decodeJSON[gen.BackupRestoreConfirm](w, r)
	if !ok || !confirmed(w, r, string(body.Confirmation)) || !s.reauthenticate(w, r, body.Password, body.TotpCode) {
		return
	}
	view, err := s.backupCfg.Restore.Undo(r.Context(), actorFrom(r))
	if err != nil {
		s.writeRestoreError(w, r, err)
		return
	}
	s.recordBackupAudit(r, "backup.restore.undo_requested", "backup:restore", nil)
	s.writeRestoreStatus(w, r, http.StatusAccepted, view)
}

func (s *Server) DiscardBackupRestore(w http.ResponseWriter, r *http.Request) {
	view, err := s.backupCfg.Restore.Discard(r.Context(), actorFrom(r))
	if err != nil {
		s.writeRestoreError(w, r, err)
		return
	}
	s.recordBackupAudit(r, "backup.restore.discarded", "backup:restore", nil)
	s.writeRestoreStatus(w, r, http.StatusOK, view)
}

// EndBackupMaintenance is the emergency exit for a maintenance mode a restore
// left on (its maintenance_off step failed after the swap). It is refused
// while the worker reports a running restore; a worker that does not answer
// is running nothing, and its next start unwinds any half-done restore.
func (s *Server) EndBackupMaintenance(w http.ResponseWriter, r *http.Request) {
	view, err := s.backupCfg.Restore.State(r.Context())
	workerUp := !errors.Is(err, backup.ErrWorkerUnavailable)
	if err != nil && workerUp {
		s.writeRestoreError(w, r, err)
		return
	}
	if view != nil && view.Phase == recovery.PhaseRunning {
		s.writeRestoreError(w, r, recovery.ErrRestoreActive)
		return
	}
	if err := backup.SetMaintenance(r.Context(), s.pool.Pool, false, ""); err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	s.recordBackupAudit(r, "backup.maintenance.ended", "backup:maintenance", map[string]any{"workerAvailable": workerUp})
	out, err := s.restoreStatus(r, view, workerUp)
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
