package apiserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/hito-hospital/hdms/internal/modules/audit/auditapi"
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
	case errors.Is(err, backup.ErrInvalidSchedule), errors.Is(err, backup.ErrInvalidDestination), errors.Is(err, backup.ErrPathNotAllowed):
		p := httpx.NewProblem("validation-error", "Validation error", http.StatusUnprocessableEntity)
		p.Detail = err.Error()
		httpx.WriteProblem(w, r, p)
	case errors.Is(err, backup.ErrDestinationExists):
		p := httpx.NewProblem("destination-exists", "A destination already uses this path", http.StatusConflict)
		httpx.WriteProblem(w, r, p)
	case errors.Is(err, backup.ErrDestinationNotFound), errors.Is(err, backup.ErrRequestNotFound):
		httpx.WriteProblem(w, r, httpx.NewProblem("not-found", "Not found", http.StatusNotFound))
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
		Schedule:     mapSchedule(sched),
		AllowedRoots: s.backupCfg.AllowedRoots,
		Local:        gen.BackupLocalRepo{Path: backup.LocalRepo(s.backupCfg.BackupDir).Location},
	}
	if out.AllowedRoots == nil {
		out.AllowedRoots = []string{}
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
	d, err := backup.CreateDestination(r.Context(), s.pool, backup.DestinationInput{
		Name: body.Name, Target: body.Target, Enabled: enabled, RetentionVersions: body.RetentionVersions,
	}, s.backupCfg.AllowedRoots, actorFrom(r))
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
