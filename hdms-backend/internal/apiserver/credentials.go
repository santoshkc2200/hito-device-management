package apiserver

import (
	"net/http"

	"github.com/hito-hospital/hdms/internal/modules/credentials/credentialsapi"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
)

func (s *Server) ListCredentialsBySubject(w http.ResponseWriter, r *http.Request, params gen.ListCredentialsBySubjectParams) {
	credentials, err := s.credentials.ListBySubject(r.Context(), credentialsapi.SubjectType(params.SubjectType), params.SubjectId)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	items := make([]gen.Credential, 0, len(credentials))
	for _, c := range credentials {
		if params.Status != nil && c.Status != credentialsapi.Status(*params.Status) {
			continue
		}
		items = append(items, credentialToGen(c))
	}
	writeJSON(w, http.StatusOK, gen.CredentialList{Items: items})
}

func (s *Server) IssueCredential(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[gen.IssueCredentialRequest](w, r)
	if !ok {
		return
	}
	issued, err := s.credentials.Issue(r.Context(), credentialsapi.IssueParams{
		SubjectType: credentialsapi.SubjectType(req.SubjectType),
		SubjectID:   fromPtr(req.SubjectId),
		Kind:        credentialsapi.Kind(req.Kind),
		Label:       fromPtr(req.Label),
		ManualToken: fromPtr(req.ManualToken),
		IssuedBy:    actorFrom(r),
	})
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, issuedCredentialToGen(issued))
}

func (s *Server) IssueBlankBatch(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[gen.IssueBlankBatchRequest](w, r)
	if !ok {
		return
	}
	batch, err := s.credentials.IssueBlankBatch(r.Context(), req.Count, credentialsapi.Kind(req.Kind), actorFrom(r))
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	items := make([]gen.IssuedCredential, 0, len(batch))
	for _, c := range batch {
		items = append(items, issuedCredentialToGen(c))
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, gen.IssueBlankBatchResponse{Items: items})
}

func (s *Server) BindCredential(w http.ResponseWriter, r *http.Request, id gen.IDParam) {
	req, ok := decodeJSON[gen.BindCredentialRequest](w, r)
	if !ok {
		return
	}
	credential, err := s.credentials.Bind(r.Context(), id, req.SubjectId, actorFrom(r))
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, credentialToGen(credential))
}

func (s *Server) ReprintCredential(w http.ResponseWriter, r *http.Request, id gen.IDParam) {
	issued, err := s.credentials.Reprint(r.Context(), id, actorFrom(r))
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, issuedCredentialToGen(issued))
}

func (s *Server) RevokeCredential(w http.ResponseWriter, r *http.Request, id gen.IDParam) {
	req, ok := decodeJSON[gen.RevokeCredentialRequest](w, r)
	if !ok {
		return
	}
	if !requireReason(w, r, req.Reason) {
		return
	}
	credential, err := s.credentials.Revoke(r.Context(), id, req.Reason, actorFrom(r))
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, credentialToGen(credential))
}

func (s *Server) ReissueCredential(w http.ResponseWriter, r *http.Request, id gen.IDParam) {
	req, ok := decodeJSON[gen.ReissueCredentialRequest](w, r)
	if !ok {
		return
	}
	if !requireReason(w, r, req.Reason) {
		return
	}
	issued, err := s.credentials.Reissue(r.Context(), id, req.Reason, actorFrom(r), fromPtr(req.ManualToken))
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, issuedCredentialToGen(issued))
}

func (s *Server) GetUnboundCredentialCount(w http.ResponseWriter, r *http.Request) {
	count, err := s.credentials.CountUnbound(r.Context())
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, gen.UnboundCountResponse{Count: count})
}

func (s *Server) ResolveCredential(w http.ResponseWriter, r *http.Request, params gen.ResolveCredentialParams) {
	ref, err := s.credentials.Resolve(r.Context(), params.Token)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, gen.ResolvedCredential{
		Type:             gen.ResolvedCredentialType(ref.Type),
		SubjectId:        strPtr(ref.SubjectID),
		CredentialId:     ref.CredentialID,
		CredentialStatus: gen.CredentialStatus(ref.CredentialStatus),
		Kind:             gen.CredentialKind(ref.Kind),
	})
}

func (s *Server) GetCredentialHistory(w http.ResponseWriter, r *http.Request, id gen.IDParam) {
	events, err := s.credentials.ListEvents(r.Context(), id)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	items := make([]gen.CredentialEvent, 0, len(events))
	for _, e := range events {
		items = append(items, gen.CredentialEvent{
			At:     e.At,
			Kind:   e.Kind,
			Actor:  e.Actor,
			Reason: strPtr(e.Reason),
		})
	}
	writeJSON(w, http.StatusOK, gen.CredentialEventList{Items: items})
}

func credentialToGen(c credentialsapi.Credential) gen.Credential {
	return gen.Credential{
		Id:            c.ID,
		SubjectType:   gen.SubjectType(c.SubjectType),
		SubjectId:     strPtr(c.SubjectID),
		Kind:          gen.CredentialKind(c.Kind),
		TokenPreview:  strPtr(c.TokenPreview),
		Label:         strPtr(c.Label),
		Status:        gen.CredentialStatus(c.Status),
		IssueSeq:      c.IssueSeq,
		ReplacesId:    strPtr(c.ReplacesID),
		IssuedAt:      c.IssuedAt,
		IssuedBy:      c.IssuedBy,
		RevokedAt:     c.RevokedAt,
		RevokedBy:     strPtr(c.RevokedBy),
		RevokedReason: strPtr(c.RevokedReason),
		PrintedCount:  c.PrintedCount,
		LastPrintedAt: c.LastPrintedAt,
	}
}

func issuedCredentialToGen(c credentialsapi.IssuedCredential) gen.IssuedCredential {
	base := credentialToGen(c.Credential)
	return gen.IssuedCredential{
		Id:            base.Id,
		SubjectType:   base.SubjectType,
		SubjectId:     base.SubjectId,
		Kind:          base.Kind,
		TokenPreview:  base.TokenPreview,
		Label:         base.Label,
		Status:        base.Status,
		IssueSeq:      base.IssueSeq,
		ReplacesId:    base.ReplacesId,
		IssuedAt:      base.IssuedAt,
		IssuedBy:      base.IssuedBy,
		RevokedAt:     base.RevokedAt,
		RevokedBy:     base.RevokedBy,
		RevokedReason: base.RevokedReason,
		PrintedCount:  base.PrintedCount,
		LastPrintedAt: base.LastPrintedAt,
		Token:         c.Token,
	}
}
