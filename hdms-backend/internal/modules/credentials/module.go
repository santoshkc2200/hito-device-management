// Package credentials owns the credentials and credential_events tables
// (docs/03, docs/05). It never imports another module — only checkout
// orchestrates across module boundaries (.golangci.yml's
// credentials-isolation rule) — and depends on auditapi only for recording
// the mutation events every write here produces.
package credentials

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/hito-hospital/hdms/internal/modules/audit/auditapi"
	"github.com/hito-hospital/hdms/internal/modules/credentials/credentialsapi"
	"github.com/hito-hospital/hdms/internal/modules/credentials/internal/cryptox"
	"github.com/hito-hospital/hdms/internal/modules/credentials/internal/domain"
	credentialsstore "github.com/hito-hospital/hdms/internal/modules/credentials/internal/store"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/pgtypeconv"
	"github.com/hito-hospital/hdms/internal/platform/tokens"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// Service implements credentialsapi.Service against Postgres.
type Service struct {
	pool   *db.Pool
	audit  auditapi.Recorder
	pepper []byte
	encKey []byte
}

// New constructs the credentials service. pepper is the HMAC key
// (config.TokenPepper); encKey is the 32-byte AES-256-GCM key
// (config.CredentialEncKey) used only for device tokens.
func New(pool *db.Pool, audit auditapi.Recorder, pepper string, encKey []byte) *Service {
	return &Service{pool: pool, audit: audit, pepper: []byte(pepper), encKey: encKey}
}

var _ credentialsapi.Service = (*Service)(nil)

func (s *Service) Issue(ctx context.Context, params credentialsapi.IssueParams) (credentialsapi.IssuedCredential, error) {
	if params.SubjectID == "" && params.SubjectType != credentialsapi.SubjectUser {
		return credentialsapi.IssuedCredential{}, fmt.Errorf("credentials: only user credentials may be issued unbound (blank card stock)")
	}

	var result credentialsapi.IssuedCredential
	err := db.NewTxManager(s.pool).Do(ctx, func(ctx context.Context) error {
		var err error
		result, err = s.issueOne(ctx, params, 1, "")
		return err
	})
	if err != nil {
		return credentialsapi.IssuedCredential{}, err
	}
	return result, nil
}

func (s *Service) IssueBlankBatch(ctx context.Context, count int, kind credentialsapi.Kind, issuedBy string) ([]credentialsapi.IssuedCredential, error) {
	if count <= 0 {
		return nil, fmt.Errorf("credentials: count must be positive")
	}

	var batch []credentialsapi.IssuedCredential
	err := db.NewTxManager(s.pool).Do(ctx, func(ctx context.Context) error {
		for range count {
			issued, err := s.issueOne(ctx, credentialsapi.IssueParams{
				SubjectType: credentialsapi.SubjectUser,
				Kind:        kind,
				Label:       "blank card stock",
				IssuedBy:    issuedBy,
			}, 1, "")
			if err != nil {
				return err
			}
			batch = append(batch, issued)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return batch, nil
}

// issueOne mints and inserts a single credential row and its 'issued'
// event, assuming a transaction is already open on ctx. issueSeq and
// replacesID let Reissue reuse this for the replacement half of its work.
func (s *Service) issueOne(ctx context.Context, params credentialsapi.IssueParams, issueSeq int, replacesID string) (credentialsapi.IssuedCredential, error) {
	token, err := s.mintToken(params.SubjectType, params.Kind, params.ManualToken)
	if err != nil {
		return credentialsapi.IssuedCredential{}, err
	}

	subjectID, err := pgtypeconv.NullUUID(params.SubjectID)
	if err != nil {
		return credentialsapi.IssuedCredential{}, fmt.Errorf("credentials: invalid subject id: %w", err)
	}
	replaces, err := pgtypeconv.NullUUID(replacesID)
	if err != nil {
		return credentialsapi.IssuedCredential{}, fmt.Errorf("credentials: invalid replaces id: %w", err)
	}

	var tokenEnc []byte
	if params.SubjectType == credentialsapi.SubjectDevice {
		tokenEnc, err = cryptox.Encrypt(token, s.encKey)
		if err != nil {
			return credentialsapi.IssuedCredential{}, fmt.Errorf("credentials: encrypt device token: %w", err)
		}
	}

	q := credentialsstore.New(db.Conn(ctx, s.pool))
	row, err := q.CreateCredential(ctx, credentialsstore.CreateCredentialParams{
		ID:           pgtypeconv.NewUUID(),
		SubjectType:  credentialsstore.SubjectType(params.SubjectType),
		SubjectID:    subjectID,
		Kind:         credentialsstore.CredentialKind(params.Kind),
		TokenHash:    cryptox.HashToken(token, s.pepper),
		TokenPreview: preview(token),
		TokenEnc:     tokenEnc,
		Label:        pgtypeconv.Text(params.Label),
		IssueSeq:     int32(issueSeq),
		ReplacesID:   replaces,
		IssuedBy:     params.IssuedBy,
	})
	if err != nil {
		return credentialsapi.IssuedCredential{}, translateCredentialErr(err)
	}

	eventPayload := map[string]any{"kind": string(params.Kind)}
	if replacesID != "" {
		eventPayload["replaces"] = replacesID
	}
	if err := s.insertEvent(ctx, row.ID, "issued", params.IssuedBy, "", eventPayload); err != nil {
		return credentialsapi.IssuedCredential{}, err
	}

	cred := toCredential(row)
	if err := s.audit.Record(ctx, auditapi.Event{
		Actor: params.IssuedBy, Action: "credential.issued", Subject: subjectOrCredential(cred),
		Payload: map[string]any{"credentialId": cred.ID, "kind": string(cred.Kind)},
	}); err != nil {
		return credentialsapi.IssuedCredential{}, err
	}

	return credentialsapi.IssuedCredential{Credential: cred, Token: token}, nil
}

// mintToken produces the plaintext token for a new credential: generated
// for qr/code128, caller-supplied for manual, and not yet issuable for
// nfc/rfid (Phase 6).
func (s *Service) mintToken(subjectType credentialsapi.SubjectType, kind credentialsapi.Kind, manualToken string) (string, error) {
	dk := domain.Kind(kind)
	switch {
	case dk.Generated():
		hint := tokens.HintUser
		if subjectType == credentialsapi.SubjectDevice {
			hint = tokens.HintDevice
		}
		tok, err := tokens.Generate(hint)
		if err != nil {
			return "", fmt.Errorf("credentials: generate token: %w", err)
		}
		return tok.String(), nil
	case kind == credentialsapi.KindManual:
		v := strings.TrimSpace(manualToken)
		if v == "" {
			return "", credentialsapi.ErrManualTokenRequired
		}
		return v, nil
	default:
		return "", credentialsapi.ErrKindNotIssuableInV1
	}
}

func (s *Service) Resolve(ctx context.Context, token string) (credentialsapi.SubjectRef, error) {
	hash := cryptox.HashToken(canonicalToken(token), s.pepper)

	q := credentialsstore.New(db.Conn(ctx, s.pool))
	row, err := q.GetCredentialByTokenHash(ctx, hash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return credentialsapi.SubjectRef{}, credentialsapi.ErrCredentialUnknown
		}
		return credentialsapi.SubjectRef{}, fmt.Errorf("credentials: resolve: %w", err)
	}

	ref := credentialsapi.SubjectRef{
		CredentialID:     pgtypeconv.UUIDString(row.ID),
		CredentialStatus: credentialsapi.Status(row.Status),
		Kind:             credentialsapi.Kind(row.Kind),
	}
	if !row.SubjectID.Valid {
		ref.Type = credentialsapi.RefUnbound
		return ref, nil
	}
	ref.SubjectID = pgtypeconv.UUIDString(row.SubjectID)
	if row.SubjectType == credentialsstore.SubjectTypeDevice {
		ref.Type = credentialsapi.RefDevice
	} else {
		ref.Type = credentialsapi.RefUser
	}
	return ref, nil
}

// canonicalToken normalises a scanned/typed string the same way the token
// codec does when it recognises the HD- format, so a lowercase or
// ambiguous-character scan of a qr/code128 token still hashes to the value
// Issue stored. A string that is not in that format (manual, or a future
// nfc/rfid UID, which the caller normalises with domain.NormalizeUID
// first) is hashed as given, trimmed only.
func canonicalToken(raw string) string {
	if tok, err := tokens.Parse(raw); err == nil {
		return tok.String()
	}
	return strings.TrimSpace(raw)
}

func (s *Service) Bind(ctx context.Context, credentialID, subjectID, actor string) (credentialsapi.Credential, error) {
	cid, err := pgtypeconv.UUID(credentialID)
	if err != nil {
		return credentialsapi.Credential{}, fmt.Errorf("credentials: invalid credential id: %w", err)
	}
	sid, err := pgtypeconv.UUID(subjectID)
	if err != nil {
		return credentialsapi.Credential{}, fmt.Errorf("credentials: invalid subject id: %w", err)
	}

	var cred credentialsapi.Credential
	err = db.NewTxManager(s.pool).Do(ctx, func(ctx context.Context) error {
		q := credentialsstore.New(db.Conn(ctx, s.pool))
		row, err := q.BindCredential(ctx, credentialsstore.BindCredentialParams{ID: cid, SubjectID: sid})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return credentialsapi.ErrCredentialNotUnbound
			}
			return fmt.Errorf("credentials: bind: %w", err)
		}
		cred = toCredential(row)

		if err := s.insertEvent(ctx, row.ID, "bound", actor, "", map[string]any{"subjectId": subjectID}); err != nil {
			return err
		}
		return s.audit.Record(ctx, auditapi.Event{
			Actor: actor, Action: "credential.bound", Subject: subjectOrCredential(cred),
			Payload: map[string]any{"credentialId": cred.ID},
		})
	})
	if err != nil {
		return credentialsapi.Credential{}, err
	}
	return cred, nil
}

func (s *Service) Reprint(ctx context.Context, credentialID, actor string) (credentialsapi.IssuedCredential, error) {
	cid, err := pgtypeconv.UUID(credentialID)
	if err != nil {
		return credentialsapi.IssuedCredential{}, fmt.Errorf("credentials: invalid credential id: %w", err)
	}

	var result credentialsapi.IssuedCredential
	err = db.NewTxManager(s.pool).Do(ctx, func(ctx context.Context) error {
		q := credentialsstore.New(db.Conn(ctx, s.pool))
		current, err := q.GetCredentialByID(ctx, cid)
		if err != nil {
			return translateCredentialErr(err)
		}
		if current.SubjectType != credentialsstore.SubjectTypeDevice || len(current.TokenEnc) == 0 {
			return credentialsapi.ErrNotDeviceCredential
		}
		if current.Status != credentialsstore.CredentialStatusActive {
			return credentialsapi.ErrCredentialNotActive
		}

		token, err := cryptox.Decrypt(current.TokenEnc, s.encKey)
		if err != nil {
			return fmt.Errorf("credentials: decrypt for reprint: %w", err)
		}

		row, err := q.RecordCredentialPrint(ctx, cid)
		if err != nil {
			return fmt.Errorf("credentials: record print: %w", err)
		}
		cred := toCredential(row)

		if err := s.insertEvent(ctx, row.ID, "reprinted", actor, "", nil); err != nil {
			return err
		}
		if err := s.audit.Record(ctx, auditapi.Event{
			Actor: actor, Action: "credential.reprinted", Subject: subjectOrCredential(cred),
			Payload: map[string]any{"credentialId": cred.ID},
		}); err != nil {
			return err
		}

		result = credentialsapi.IssuedCredential{Credential: cred, Token: token}
		return nil
	})
	if err != nil {
		return credentialsapi.IssuedCredential{}, err
	}
	return result, nil
}

func (s *Service) Revoke(ctx context.Context, credentialID, reason, actor string) (credentialsapi.Credential, error) {
	reason, err := domain.ValidateReason(reason)
	if err != nil {
		return credentialsapi.Credential{}, err
	}
	cid, err := pgtypeconv.UUID(credentialID)
	if err != nil {
		return credentialsapi.Credential{}, fmt.Errorf("credentials: invalid credential id: %w", err)
	}

	var cred credentialsapi.Credential
	err = db.NewTxManager(s.pool).Do(ctx, func(ctx context.Context) error {
		q := credentialsstore.New(db.Conn(ctx, s.pool))
		row, err := q.RevokeCredential(ctx, credentialsstore.RevokeCredentialParams{
			ID: cid, RevokedBy: pgtypeconv.Text(actor), RevokedReason: pgtypeconv.Text(reason),
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return credentialsapi.ErrCredentialNotActive
			}
			return fmt.Errorf("credentials: revoke: %w", err)
		}
		cred = toCredential(row)

		if err := s.insertEvent(ctx, row.ID, "revoked", actor, reason, nil); err != nil {
			return err
		}
		return s.audit.Record(ctx, auditapi.Event{
			Actor: actor, Action: "credential.revoked", Subject: subjectOrCredential(cred),
			Payload: map[string]any{"credentialId": cred.ID, "reason": reason},
		})
	})
	if err != nil {
		return credentialsapi.Credential{}, err
	}
	return cred, nil
}

func (s *Service) Reissue(ctx context.Context, credentialID, reason, actor, manualToken string) (credentialsapi.IssuedCredential, error) {
	reason, err := domain.ValidateReason(reason)
	if err != nil {
		return credentialsapi.IssuedCredential{}, err
	}
	cid, err := pgtypeconv.UUID(credentialID)
	if err != nil {
		return credentialsapi.IssuedCredential{}, fmt.Errorf("credentials: invalid credential id: %w", err)
	}

	var result credentialsapi.IssuedCredential
	err = db.NewTxManager(s.pool).Do(ctx, func(ctx context.Context) error {
		q := credentialsstore.New(db.Conn(ctx, s.pool))
		current, err := q.GetCredentialByID(ctx, cid)
		if err != nil {
			return translateCredentialErr(err)
		}
		if current.Status != credentialsstore.CredentialStatusActive {
			return credentialsapi.ErrCredentialNotActive
		}

		old, err := q.MarkCredentialLostForReissue(ctx, credentialsstore.MarkCredentialLostForReissueParams{
			ID: cid, RevokedBy: pgtypeconv.Text(actor), RevokedReason: pgtypeconv.Text(reason),
		})
		if err != nil {
			return fmt.Errorf("credentials: mark lost: %w", err)
		}

		issued, err := s.issueOne(ctx, credentialsapi.IssueParams{
			SubjectType: credentialsapi.SubjectType(old.SubjectType),
			SubjectID:   pgtypeconv.UUIDString(old.SubjectID),
			Kind:        credentialsapi.Kind(old.Kind),
			Label:       pgtypeconv.TextString(old.Label),
			ManualToken: manualToken,
			IssuedBy:    actor,
		}, int(old.IssueSeq)+1, pgtypeconv.UUIDString(old.ID))
		if err != nil {
			return err
		}

		if err := s.insertEvent(ctx, old.ID, "reissued", actor, reason, map[string]any{"newCredentialId": issued.ID}); err != nil {
			return err
		}
		if err := s.audit.Record(ctx, auditapi.Event{
			Actor: actor, Action: "credential.reissued", Subject: subjectOrCredential(issued.Credential),
			Payload: map[string]any{"oldCredentialId": pgtypeconv.UUIDString(old.ID), "newCredentialId": issued.ID, "reason": reason},
		}); err != nil {
			return err
		}

		result = issued
		return nil
	})
	if err != nil {
		return credentialsapi.IssuedCredential{}, err
	}
	return result, nil
}

func (s *Service) ListBySubject(ctx context.Context, subjectType credentialsapi.SubjectType, subjectID string) ([]credentialsapi.Credential, error) {
	sid, err := pgtypeconv.UUID(subjectID)
	if err != nil {
		return nil, fmt.Errorf("credentials: invalid subject id: %w", err)
	}
	q := credentialsstore.New(db.Conn(ctx, s.pool))
	rows, err := q.ListCredentialsBySubject(ctx, credentialsstore.ListCredentialsBySubjectParams{
		SubjectType: credentialsstore.SubjectType(subjectType), SubjectID: sid,
	})
	if err != nil {
		return nil, fmt.Errorf("credentials: list by subject: %w", err)
	}
	creds := make([]credentialsapi.Credential, 0, len(rows))
	for _, r := range rows {
		creds = append(creds, toCredential(r))
	}
	return creds, nil
}

func (s *Service) ListEvents(ctx context.Context, credentialID string) ([]credentialsapi.Event, error) {
	cid, err := pgtypeconv.UUID(credentialID)
	if err != nil {
		return nil, fmt.Errorf("credentials: invalid credential id: %w", err)
	}
	q := credentialsstore.New(db.Conn(ctx, s.pool))
	rows, err := q.ListCredentialEvents(ctx, cid)
	if err != nil {
		return nil, fmt.Errorf("credentials: list events: %w", err)
	}
	events := make([]credentialsapi.Event, 0, len(rows))
	for _, r := range rows {
		events = append(events, credentialsapi.Event{
			At: pgtypeconv.Time(r.At), Kind: r.Kind, Actor: r.Actor, Reason: pgtypeconv.TextString(r.Reason),
		})
	}
	return events, nil
}

func (s *Service) CountUnbound(ctx context.Context) (int, error) {
	q := credentialsstore.New(db.Conn(ctx, s.pool))
	n, err := q.CountUnboundCredentials(ctx)
	if err != nil {
		return 0, fmt.Errorf("credentials: count unbound: %w", err)
	}
	return int(n), nil
}

func (s *Service) insertEvent(ctx context.Context, credentialID pgtype.UUID, kind, actor, reason string, payload map[string]any) error {
	data, err := marshalPayload(payload)
	if err != nil {
		return fmt.Errorf("credentials: marshal event payload: %w", err)
	}
	q := credentialsstore.New(db.Conn(ctx, s.pool))
	return q.InsertCredentialEvent(ctx, credentialsstore.InsertCredentialEventParams{
		ID:           pgtypeconv.NewUUID(),
		CredentialID: credentialID,
		Kind:         kind,
		Actor:        actor,
		Reason:       pgtypeconv.Text(reason),
		Payload:      data,
	})
}

func toCredential(row credentialsstore.Credential) credentialsapi.Credential {
	return credentialsapi.Credential{
		ID:            pgtypeconv.UUIDString(row.ID),
		SubjectType:   credentialsapi.SubjectType(row.SubjectType),
		SubjectID:     pgtypeconv.UUIDString(row.SubjectID),
		Kind:          credentialsapi.Kind(row.Kind),
		TokenPreview:  row.TokenPreview,
		Label:         pgtypeconv.TextString(row.Label),
		Status:        credentialsapi.Status(row.Status),
		IssueSeq:      int(row.IssueSeq),
		ReplacesID:    pgtypeconv.UUIDString(row.ReplacesID),
		IssuedAt:      pgtypeconv.Time(row.IssuedAt),
		IssuedBy:      row.IssuedBy,
		RevokedAt:     pgtypeconv.TimePtr(row.RevokedAt),
		RevokedBy:     pgtypeconv.TextString(row.RevokedBy),
		RevokedReason: pgtypeconv.TextString(row.RevokedReason),
		PrintedCount:  int(row.PrintedCount),
		LastPrintedAt: pgtypeconv.TimePtr(row.LastPrintedAt),
	}
}

func subjectOrCredential(c credentialsapi.Credential) string {
	if c.SubjectID == "" {
		return "credential:" + c.ID
	}
	return string(c.SubjectType) + ":" + c.SubjectID
}

func preview(token string) string {
	if len(token) <= 4 {
		return token
	}
	return token[len(token)-4:]
}

func marshalPayload(payload map[string]any) ([]byte, error) {
	if payload == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(payload)
}

func translateCredentialErr(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return credentialsapi.ErrCredentialNotFound
	}
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == "23505" {
		return credentialsapi.ErrTokenAlreadyRegistered
	}
	return err
}
