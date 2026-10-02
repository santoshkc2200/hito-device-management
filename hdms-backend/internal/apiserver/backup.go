package apiserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/hito-hospital/hdms/internal/modules/audit/auditapi"
	"github.com/hito-hospital/hdms/internal/platform/auth"
	"github.com/hito-hospital/hdms/internal/platform/backup"
	"github.com/hito-hospital/hdms/internal/platform/httpx"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
)

func (s *Server) backupLoc() *time.Location {
	if s.backupCfg.Location != nil {
		return s.backupCfg.Location
	}
	return time.Local
}

// writeBackupError maps backup package errors to problems; anything else
// goes through the shared mapper.
func (s *Server) writeBackupError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, backup.ErrInvalidSchedule), errors.Is(err, backup.ErrInvalidDestination),
		errors.Is(err, backup.ErrPathNotAllowed), errors.Is(err, backup.ErrInvalidFolderName):
		p := httpx.NewProblem("validation-error", "Validation error", http.StatusUnprocessableEntity)
		p.Detail = err.Error()
		httpx.WriteProblem(w, r, p)
	case errors.Is(err, backup.ErrDestinationExists):
		p := httpx.NewProblem("destination-exists", "A destination already uses this path", http.StatusConflict)
		httpx.WriteProblem(w, r, p)
	case errors.Is(err, backup.ErrFolderExists):
		httpx.WriteProblem(w, r, httpx.NewProblem("folder-exists", "A folder with that name already exists", http.StatusConflict))
	case errors.Is(err, backup.ErrLocationNotWritable):
		httpx.WriteProblem(w, r, httpx.NewProblem("folder-not-writable", "HDMS cannot write in this folder", http.StatusUnprocessableEntity))
	case errors.Is(err, backup.ErrDriveNotConnected):
		httpx.WriteProblem(w, r, httpx.NewProblem("drive-not-connected", "The drive is not connected", http.StatusConflict))
	case errors.Is(err, backup.ErrWorkerUnavailable):
		httpx.WriteProblem(w, r, httpx.NewProblem("worker-unavailable", "Backup worker not responding", http.StatusServiceUnavailable))
	case errors.Is(err, backup.ErrDestinationNotFound), errors.Is(err, backup.ErrRequestNotFound), errors.Is(err, backup.ErrLocationNotFound):
		httpx.WriteProblem(w, r, httpx.NewProblem("not-found", "Not found", http.StatusNotFound))
	case errors.Is(err, backup.ErrNoRecoveryKey):
		httpx.WriteProblem(w, r, httpx.NewProblem("not-found", "No recovery key has been created", http.StatusNotFound))
	default:
		s.writeServiceError(w, r, err)
	}
}

func parseBackupID(w http.ResponseWriter, r *http.Request, raw string) (uuid.UUID, bool) {
	id, err := uuid.Parse(raw)
	if err != nil {
		httpx.WriteProblem(w, r, httpx.NewProblem("not-found", "Not found", http.StatusNotFound))
		return uuid.UUID{}, false
	}
	return id, true
}

func (s *Server) recordBackupAudit(r *http.Request, action, subject string, payload map[string]any) {
	_ = s.audit.Record(r.Context(), auditapi.Event{
		Actor:   actorFrom(r),
		Action:  action,
		Subject: subject,
		Payload: payload,
	})
}

func mapSchedule(c backup.ScheduleConfig) gen.BackupSchedule {
	return gen.BackupSchedule{
		Enabled:         c.Enabled,
		Mode:            gen.BackupScheduleMode(c.Mode),
		IntervalMinutes: c.IntervalMinutes,
		TimeLocal:       c.TimeLocal,
		Weekday:         c.Weekday,
	}
}

func mapRun(run backup.Run) gen.BackupRun {
	detail := map[string]interface{}{}
	_ = json.Unmarshal(run.Detail, &detail)
	return gen.BackupRun{
		Id:         run.ID,
		Job:        gen.BackupRunJob(run.Job),
		StartedAt:  run.StartedAt,
		FinishedAt: run.FinishedAt,
		Outcome:    gen.BackupRunOutcome(run.Outcome),
		Detail:     detail,
	}
}

func mapBackupRequest(req backup.Request) gen.BackupRequest {
	out := gen.BackupRequest{
		Id:          req.ID.String(),
		Kind:        gen.BackupRequestKind(req.Kind),
		Status:      gen.BackupRequestStatus(req.Status()),
		RequestedAt: req.RequestedAt,
		StartedAt:   req.StartedAt,
		FinishedAt:  req.FinishedAt,
	}
	if req.DestinationID != nil {
		out.DestinationId = strPtr(req.DestinationID.String())
	}
	if req.Outcome != "" {
		o := gen.BackupRequestOutcome(req.Outcome)
		out.Outcome = &o
	}
	if len(req.Detail) > 0 {
		detail := map[string]interface{}{}
		if json.Unmarshal(req.Detail, &detail) == nil {
			out.Detail = &detail
		}
	}
	return out
}

func mapBackupDestination(d backup.Destination) gen.BackupDestination {
	out := gen.BackupDestination{
		Id:                d.ID.String(),
		Name:              d.Name,
		Target:            d.Target,
		Enabled:           d.Enabled,
		RetentionVersions: d.RetentionVersions,
		InitializedAt:     d.InitializedAt,
		LastOkAt:          d.LastOkAt,
	}
	if d.LastError != "" {
		out.LastError = strPtr(d.LastError)
	}
	return out
}

func (s *Server) backupConfig(r *http.Request) (gen.BackupConfig, error) {
	ctx := r.Context()
	sched, err := backup.GetSchedule(ctx, s.pool.Pool)
	if err != nil {
		return gen.BackupConfig{}, err
	}
	out := gen.BackupConfig{
		Schedule:  mapSchedule(sched),
		DrivesDir: s.backupCfg.DrivesDir,
		Local:     gen.BackupLocalRepo{Path: backup.LocalRepo(s.backupCfg.BackupDir).Location},
	}
	if s.backupCfg.DrivesHostPath != "" {
		out.DrivesHostPath = strPtr(s.backupCfg.DrivesHostPath)
	}
	if next, ok := sched.NextAfter(time.Now(), s.backupLoc()); ok {
		out.NextRunAt = &next
	}
	runs, err := backup.RecentRuns(ctx, s.pool.Pool, 20)
	if err != nil {
		return gen.BackupConfig{}, err
	}
	for _, run := range runs {
		if run.Job == "backup" {
			last := mapRun(run)
			out.LastRun = &last
			break
		}
	}
	if out.LastSuccessAt, err = backup.LastSuccessfulBackup(ctx, s.pool.Pool); err != nil {
		return gen.BackupConfig{}, err
	}
	if out.WorkerSeenAt, err = backup.WorkerSeenAt(ctx, s.pool.Pool); err != nil {
		return gen.BackupConfig{}, err
	}
	snaps, err := backup.ListSnapshots(ctx, s.pool.Pool, backup.LocalRepoKey)
	if err != nil {
		return gen.BackupConfig{}, err
	}
	out.Local.SnapshotCount = len(snaps)
	if len(snaps) > 0 {
		size := snaps[0].SizeBytes
		out.Local.LatestSizeBytes = &size
		out.Local.VerifiedAt = snaps[0].VerifiedAt
	}
	out.RecoveryKey, err = s.recoveryKeyState(ctx)
	if err != nil {
		return gen.BackupConfig{}, err
	}
	return out, nil
}

func (s *Server) GetBackupConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.backupConfig(r)
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) UpdateBackupSchedule(w http.ResponseWriter, r *http.Request) {
	body, ok := decodeJSON[gen.BackupSchedule](w, r)
	if !ok {
		return
	}
	before, err := backup.GetSchedule(r.Context(), s.pool.Pool)
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	after, err := backup.SaveSchedule(r.Context(), s.pool.Pool, backup.ScheduleConfig{
		Enabled:         body.Enabled,
		Mode:            string(body.Mode),
		IntervalMinutes: body.IntervalMinutes,
		TimeLocal:       body.TimeLocal,
		Weekday:         body.Weekday,
	}, actorFrom(r))
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	s.recordBackupAudit(r, "backup.schedule_updated", "backup:schedule", map[string]any{
		"before": mapSchedule(before), "after": mapSchedule(after),
	})
	s.GetBackupConfig(w, r)
}

func (s *Server) ListBackupDestinations(w http.ResponseWriter, r *http.Request) {
	dests, err := backup.LoadAllDestinations(r.Context(), s.pool)
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	items := make([]gen.BackupDestination, 0, len(dests))
	for _, d := range dests {
		items = append(items, mapBackupDestination(d))
	}
	writeJSON(w, http.StatusOK, gen.BackupDestinationList{Items: items})
}

func (s *Server) CreateBackupDestination(w http.ResponseWriter, r *http.Request) {
	body, ok := decodeJSON[gen.BackupDestinationInput](w, r)
	if !ok {
		return
	}
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	// The wizard already checked the folder; check again so the rules hold
	// for any caller, and so a folder that changed since is caught.
	check, err := s.backupCfg.Locations.Check(r.Context(), body.Target)
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	if !check.OK {
		p := httpx.NewProblem("location-check-failed", "The folder did not pass the checks", http.StatusUnprocessableEntity)
		p.Detail = strings.Join(check.FailedCodes(), ", ")
		httpx.WriteProblem(w, r, p)
		return
	}
	d, err := backup.CreateDestination(r.Context(), s.pool, backup.DestinationInput{
		Name: body.Name, Target: body.Target, Enabled: enabled, RetentionVersions: body.RetentionVersions,
	}, actorFrom(r))
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	s.recordBackupAudit(r, "backup.destination_created", "backup_destination:"+d.ID.String(), map[string]any{
		"name": d.Name, "target": d.Target, "retentionVersions": d.RetentionVersions,
	})
	writeJSON(w, http.StatusCreated, mapBackupDestination(d))
}

func (s *Server) UpdateBackupDestination(w http.ResponseWriter, r *http.Request, rawID gen.IDParam) {
	id, ok := parseBackupID(w, r, rawID)
	if !ok {
		return
	}
	body, ok := decodeJSON[gen.BackupDestinationUpdate](w, r)
	if !ok {
		return
	}
	before, err := backup.GetDestination(r.Context(), s.pool, id)
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	after, err := backup.UpdateDestination(r.Context(), s.pool, id, backup.DestinationInput{
		Name: body.Name, Enabled: body.Enabled, RetentionVersions: body.RetentionVersions,
	}, actorFrom(r))
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	s.recordBackupAudit(r, "backup.destination_updated", "backup_destination:"+id.String(), map[string]any{
		"before": mapBackupDestination(before), "after": mapBackupDestination(after),
	})
	writeJSON(w, http.StatusOK, mapBackupDestination(after))
}

func (s *Server) DeleteBackupDestination(w http.ResponseWriter, r *http.Request, rawID gen.IDParam) {
	id, ok := parseBackupID(w, r, rawID)
	if !ok {
		return
	}
	before, err := backup.GetDestination(r.Context(), s.pool, id)
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	if err := backup.DeleteDestination(r.Context(), s.pool, id); err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	s.recordBackupAudit(r, "backup.destination_deleted", "backup_destination:"+id.String(), map[string]any{
		"name": before.Name, "target": before.Target,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) enqueueBackup(w http.ResponseWriter, r *http.Request, kind string, dest *uuid.UUID) {
	req, err := backup.EnqueueRequest(r.Context(), s.pool.Pool, kind, dest, actorFrom(r))
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	payload := map[string]any{"requestId": req.ID.String()}
	if dest != nil {
		payload["destinationId"] = dest.String()
	}
	s.recordBackupAudit(r, "backup."+kind+"_requested", "backup_request:"+req.ID.String(), payload)
	writeJSON(w, http.StatusAccepted, mapBackupRequest(req))
}

func (s *Server) TestBackupDestination(w http.ResponseWriter, r *http.Request, rawID gen.IDParam) {
	id, ok := parseBackupID(w, r, rawID)
	if !ok {
		return
	}
	if _, err := backup.GetDestination(r.Context(), s.pool, id); err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	s.enqueueBackup(w, r, backup.RequestTest, &id)
}

func (s *Server) RunBackupNow(w http.ResponseWriter, r *http.Request) {
	s.enqueueBackup(w, r, backup.RequestRun, nil)
}

func (s *Server) VerifyBackups(w http.ResponseWriter, r *http.Request) {
	var body gen.VerifyBackupsRequest
	if r.ContentLength > 0 {
		parsed, ok := decodeJSON[gen.VerifyBackupsRequest](w, r)
		if !ok {
			return
		}
		body = parsed
	}
	var dest *uuid.UUID
	if body.DestinationId != nil && *body.DestinationId != "" {
		id, ok := parseBackupID(w, r, *body.DestinationId)
		if !ok {
			return
		}
		if _, err := backup.GetDestination(r.Context(), s.pool, id); err != nil {
			s.writeBackupError(w, r, err)
			return
		}
		dest = &id
	}
	s.enqueueBackup(w, r, backup.RequestVerify, dest)
}

func (s *Server) ListBackupSnapshots(w http.ResponseWriter, r *http.Request, params gen.ListBackupSnapshotsParams) {
	key := params.Repo
	if key != backup.LocalRepoKey {
		if _, err := uuid.Parse(key); err != nil {
			s.writeBackupError(w, r, backup.ErrInvalidDestination)
			return
		}
	}
	snaps, err := backup.ListSnapshots(r.Context(), s.pool.Pool, key)
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	items := make([]gen.BackupSnapshot, 0, len(snaps))
	for _, sn := range snaps {
		items = append(items, gen.BackupSnapshot{
			SnapshotId: sn.SnapshotID, TakenAt: sn.TakenAt, SizeBytes: sn.SizeBytes, VerifiedAt: sn.VerifiedAt,
		})
	}
	writeJSON(w, http.StatusOK, gen.BackupSnapshotList{Items: items})
}

func (s *Server) GetBackupRequest(w http.ResponseWriter, r *http.Request, rawID gen.IDParam) {
	id, ok := parseBackupID(w, r, rawID)
	if !ok {
		return
	}
	req, err := backup.GetRequest(r.Context(), s.pool.Pool, id)
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, mapBackupRequest(req))
}

func (s *Server) ListBackupRuns(w http.ResponseWriter, r *http.Request, params gen.ListBackupRunsParams) {
	limit := 20
	if params.Limit != nil && *params.Limit >= 1 && *params.Limit <= 100 {
		limit = *params.Limit
	}
	runs, err := backup.RecentRuns(r.Context(), s.pool.Pool, limit)
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	items := make([]gen.BackupRun, 0, len(runs))
	for _, run := range runs {
		items = append(items, mapRun(run))
	}
	writeJSON(w, http.StatusOK, gen.BackupRunList{Items: items})
}

func mapLocationListing(l backup.LocationListing) gen.BackupLocationListing {
	out := gen.BackupLocationListing{
		Roots:   make([]gen.BackupLocationRoot, 0, len(l.Roots)),
		Folders: make([]gen.BackupFolder, 0, len(l.Folders)),
	}
	for _, root := range l.Roots {
		r := gen.BackupLocationRoot{Path: root.Path, Name: root.Name, Connected: root.Connected}
		if root.HostPath != "" {
			r.HostPath = strPtr(root.HostPath)
		}
		out.Roots = append(out.Roots, r)
	}
	for _, f := range l.Folders {
		out.Folders = append(out.Folders, gen.BackupFolder{Name: f.Name, Path: f.Path, HasBackup: f.HasBackup})
	}
	if l.Path != "" {
		out.Path = strPtr(l.Path)
	}
	if l.Parent != "" {
		out.Parent = strPtr(l.Parent)
	}
	return out
}

func mapLocationCheck(c backup.LocationCheck) gen.BackupLocationCheck {
	out := gen.BackupLocationCheck{
		Path: c.Path, Ok: c.OK, ExistingRepo: c.ExistingRepo,
		FreeBytes: c.FreeBytes, NeededBytes: c.NeededBytes,
		Checks: make([]gen.BackupLocationCheckItem, 0, len(c.Checks)),
	}
	for _, it := range c.Checks {
		item := gen.BackupLocationCheckItem{
			Name:   gen.BackupLocationCheckItemName(it.Name),
			Status: gen.BackupLocationCheckItemStatus(it.Status),
		}
		if it.Code != "" {
			code := gen.BackupLocationCheckItemCode(it.Code)
			item.Code = &code
		}
		out.Checks = append(out.Checks, item)
	}
	return out
}

func (s *Server) ListBackupLocations(w http.ResponseWriter, r *http.Request, params gen.ListBackupLocationsParams) {
	path := ""
	if params.Path != nil {
		path = *params.Path
	}
	listing, err := s.backupCfg.Locations.Browse(r.Context(), path)
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, mapLocationListing(listing))
}

func (s *Server) CreateBackupLocationFolder(w http.ResponseWriter, r *http.Request) {
	body, ok := decodeJSON[gen.BackupFolderInput](w, r)
	if !ok {
		return
	}
	f, err := s.backupCfg.Locations.CreateFolder(r.Context(), body.Parent, body.Name)
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	s.recordBackupAudit(r, "backup.location_folder_created", "backup_location:"+f.Path, map[string]any{"path": f.Path})
	writeJSON(w, http.StatusCreated, gen.BackupFolder{Name: f.Name, Path: f.Path, HasBackup: f.HasBackup})
}

func (s *Server) CheckBackupLocation(w http.ResponseWriter, r *http.Request) {
	body, ok := decodeJSON[gen.BackupLocationCheckInput](w, r)
	if !ok {
		return
	}
	check, err := s.backupCfg.Locations.Check(r.Context(), body.Path)
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, mapLocationCheck(check))
}

func mapRecoveryKeyState(rec *backup.RecoveryKeyRecord, current backup.RecoverySecrets) gen.BackupRecoveryKeyState {
	out := gen.BackupRecoveryKeyState{Status: gen.BackupRecoveryKeyStateStatus(backup.RecoveryKeyStatus(rec, current))}
	if rec != nil {
		out.CreatedAt = &rec.CreatedAt
		out.CreatedBy = strPtr(rec.CreatedBy)
		out.ConfirmedAt = rec.ConfirmedAt
	}
	return out
}

func (s *Server) recoveryKeyState(ctx context.Context) (gen.BackupRecoveryKeyState, error) {
	rec, err := backup.GetRecoveryKey(ctx, s.pool.Pool)
	if errors.Is(err, backup.ErrNoRecoveryKey) {
		return mapRecoveryKeyState(nil, s.backupCfg.RecoverySecrets), nil
	}
	if err != nil {
		return gen.BackupRecoveryKeyState{}, err
	}
	return mapRecoveryKeyState(&rec, s.backupCfg.RecoverySecrets), nil
}

// CreateBackupRecoveryKey issues a new recovery key after re-authentication.
// The key leaves the server in this one response and is never stored.
func (s *Server) CreateBackupRecoveryKey(w http.ResponseWriter, r *http.Request) {
	body, ok := decodeJSON[gen.BackupReauth](w, r)
	if !ok {
		return
	}
	admin, ok := auth.AdminFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.NewProblem("unauthorized", "Unauthorized", http.StatusUnauthorized))
		return
	}
	if err := s.auth.ReauthenticateAdmin(r.Context(), admin.ID, body.Password, body.TotpCode); err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidCredentials):
			// 422, not 401: the console treats 401 as an expired session.
			httpx.WriteProblem(w, r, httpx.NewProblem("reauth-failed", "Password or code is incorrect", http.StatusUnprocessableEntity))
		default:
			s.writeServiceError(w, r, err)
		}
		return
	}
	secrets := s.backupCfg.RecoverySecrets
	if !secrets.Complete() {
		p := httpx.NewProblem("backup-key-missing", "HDMS_BACKUP_ENC_KEY and the other secrets must be set before a recovery key can be made", http.StatusConflict)
		httpx.WriteProblem(w, r, p)
		return
	}
	key, err := backup.NewRecoveryKey()
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	bundle, err := backup.SealRecoveryBundle(key, secrets, time.Now())
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	_, prevErr := backup.GetRecoveryKey(r.Context(), s.pool.Pool)
	rec, err := backup.SaveRecoveryKey(r.Context(), s.pool.Pool, bundle, secrets.Fingerprint(), actorFrom(r))
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	action := "backup.recovery_key.replaced"
	if errors.Is(prevErr, backup.ErrNoRecoveryKey) {
		action = "backup.recovery_key.created"
	}
	s.recordBackupAudit(r, action, "backup:recovery_key", map[string]any{"createdAt": rec.CreatedAt})
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, gen.BackupRecoveryKeyIssued{Key: key.String(), CreatedAt: rec.CreatedAt})
}

func (s *Server) ConfirmBackupRecoveryKey(w http.ResponseWriter, r *http.Request) {
	rec, err := backup.ConfirmRecoveryKey(r.Context(), s.pool.Pool, time.Now())
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	s.recordBackupAudit(r, "backup.recovery_key.confirmed", "backup:recovery_key", nil)
	writeJSON(w, http.StatusOK, mapRecoveryKeyState(&rec, s.backupCfg.RecoverySecrets))
}
