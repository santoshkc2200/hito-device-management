package apiserver

import (
	"net/http"

	"github.com/hito-hospital/hdms/internal/platform/backup"
	"github.com/hito-hospital/hdms/internal/platform/httpx"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
)

// cloud returns the cloud service, or answers 503 and returns nil when cloud
// backup is not configured on this server.
func (s *Server) cloud(w http.ResponseWriter, r *http.Request) *backup.CloudService {
	if s.backupCfg.Cloud == nil {
		httpx.WriteProblem(w, r, httpx.NewProblem("cloud-unavailable", "Cloud backup is not configured", http.StatusServiceUnavailable))
		return nil
	}
	return s.backupCfg.Cloud
}

func cloudDeref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func mapCloudAccount(a backup.CloudAccount) gen.BackupCloudAccount {
	out := gen.BackupCloudAccount{
		Id:          a.ID.String(),
		Provider:    gen.BackupCloudProvider(a.Provider),
		Name:        a.Name,
		ClientId:    a.ClientID,
		Tenant:      a.Tenant,
		Status:      gen.BackupCloudAccountStatus(a.Status),
		ConnectedAt: a.ConnectedAt,
	}
	if a.AccountEmail != "" {
		out.AccountEmail = strPtr(a.AccountEmail)
	}
	if a.LastError != "" {
		out.LastError = strPtr(a.LastError)
	}
	return out
}

func mapCloudSignIn(si backup.SignIn) gen.BackupCloudSignIn {
	return gen.BackupCloudSignIn{
		Id: si.AccountID.String(), UserCode: si.UserCode, VerificationUri: si.VerificationURI, ExpiresAt: si.ExpiresAt,
	}
}

func (s *Server) ListBackupCloudAccounts(w http.ResponseWriter, r *http.Request) {
	svc := s.cloud(w, r)
	if svc == nil {
		return
	}
	accounts, err := svc.List(r.Context())
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	items := make([]gen.BackupCloudAccount, 0, len(accounts))
	for _, a := range accounts {
		items = append(items, mapCloudAccount(a))
	}
	writeJSON(w, http.StatusOK, gen.BackupCloudAccountList{Items: items})
}

func (s *Server) CreateBackupCloudAccount(w http.ResponseWriter, r *http.Request) {
	svc := s.cloud(w, r)
	if svc == nil {
		return
	}
	body, ok := decodeJSON[gen.BackupCloudAccountInput](w, r)
	if !ok {
		return
	}
	si, err := svc.Start(r.Context(), backup.CloudAccountInput{
		Provider: string(body.Provider), Name: body.Name, ClientID: body.ClientId,
		ClientSecret: cloudDeref(body.ClientSecret), Tenant: cloudDeref(body.Tenant),
	}, actorFrom(r))
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	// The payload names the account and client, never a secret.
	s.recordBackupAudit(r, "backup.cloud_account_created", "backup_cloud_account:"+si.AccountID.String(), map[string]any{
		"name": body.Name, "provider": string(body.Provider), "clientId": body.ClientId,
	})
	writeJSON(w, http.StatusCreated, mapCloudSignIn(si))
}

func (s *Server) GetBackupCloudAccount(w http.ResponseWriter, r *http.Request, rawID gen.IDParam) {
	svc := s.cloud(w, r)
	if svc == nil {
		return
	}
	id, ok := parseBackupID(w, r, rawID)
	if !ok {
		return
	}
	before, err := svc.Get(r.Context(), id)
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	after, err := svc.Poll(r.Context(), id)
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	if before.Status != backup.AccountConnected && after.Status == backup.AccountConnected {
		s.recordBackupAudit(r, "backup.cloud_account_connected", "backup_cloud_account:"+id.String(), map[string]any{
			"name": after.Name, "provider": after.Provider, "accountEmail": after.AccountEmail,
		})
	}
	writeJSON(w, http.StatusOK, mapCloudAccount(after))
}

func (s *Server) ReconnectBackupCloudAccount(w http.ResponseWriter, r *http.Request, rawID gen.IDParam) {
	svc := s.cloud(w, r)
	if svc == nil {
		return
	}
	id, ok := parseBackupID(w, r, rawID)
	if !ok {
		return
	}
	si, err := svc.Reconnect(r.Context(), id, actorFrom(r))
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	s.recordBackupAudit(r, "backup.cloud_account_reconnect_requested", "backup_cloud_account:"+id.String(), nil)
	writeJSON(w, http.StatusOK, mapCloudSignIn(si))
}

func (s *Server) DeleteBackupCloudAccount(w http.ResponseWriter, r *http.Request, rawID gen.IDParam) {
	svc := s.cloud(w, r)
	if svc == nil {
		return
	}
	id, ok := parseBackupID(w, r, rawID)
	if !ok {
		return
	}
	before, err := svc.Get(r.Context(), id)
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	if err := svc.Delete(r.Context(), id); err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	s.recordBackupAudit(r, "backup.cloud_account_deleted", "backup_cloud_account:"+id.String(), map[string]any{
		"name": before.Name, "provider": before.Provider,
	})
	w.WriteHeader(http.StatusNoContent)
}

// createCloudBackupDestination handles POST /backup/destinations when the
// body names a cloud account instead of a drive folder.
func (s *Server) createCloudBackupDestination(w http.ResponseWriter, r *http.Request, body gen.BackupDestinationInput, enabled bool) {
	svc := s.cloud(w, r)
	if svc == nil {
		return
	}
	accountID, ok := parseBackupID(w, r, *body.CloudAccountId)
	if !ok {
		return
	}
	acct, err := svc.Get(r.Context(), accountID)
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	d, err := backup.CreateCloudDestination(r.Context(), s.pool, acct, backup.CloudDestinationInput{
		Name: body.Name, Folder: *body.Folder, Enabled: enabled, RetentionVersions: body.RetentionVersions,
	}, actorFrom(r))
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	s.recordBackupAudit(r, "backup.destination_created", "backup_destination:"+d.ID.String(), map[string]any{
		"name": d.Name, "provider": d.Provider, "cloudAccountId": accountID.String(), "folder": d.Folder,
		"retentionVersions": d.RetentionVersions,
	})
	writeJSON(w, http.StatusCreated, mapBackupDestination(d))
}
