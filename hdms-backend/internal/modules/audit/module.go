// Package audit is the write-mostly sink every other module records to.
// It depends on nothing module-shaped (see .golangci.yml's audit-isolation
// rule) — only platform code and the standard library.
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/audit/auditapi"
	"github.com/hito-hospital/hdms/internal/modules/audit/internal/store"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/pgtypeconv"
	"github.com/jackc/pgx/v5/pgtype"
)

// Service implements auditapi.Service against Postgres.
type Service struct {
	pool *db.Pool
}

// New constructs the audit service. Called once from the composition root;
// every other module receives it through the auditapi.Recorder interface.
func New(pool *db.Pool) *Service {
	return &Service{pool: pool}
}

var _ auditapi.Service = (*Service)(nil)

// Record inserts one audit event, enlisting in whatever transaction is
// already on ctx so it commits atomically with the domain change it
// describes.
func (s *Service) Record(ctx context.Context, ev auditapi.Event) error {
	payload, err := marshalPayload(ev.Payload)
	if err != nil {
		return fmt.Errorf("audit: record: %w", err)
	}

	var actorIP *netip.Addr
	if ev.ActorIP != "" {
		addr, err := netip.ParseAddr(ev.ActorIP)
		if err != nil {
			return fmt.Errorf("audit: record: invalid actor IP %q: %w", ev.ActorIP, err)
		}
		actorIP = &addr
	}

	q := auditstore.New(db.Conn(ctx, s.pool))
	return q.InsertAuditEvent(ctx, auditstore.InsertAuditEventParams{
		ID:        pgtypeconv.NewUUID(),
		Actor:     ev.Actor,
		ActorIp:   actorIP,
		Action:    ev.Action,
		Subject:   ev.Subject,
		Payload:   payload,
		RequestID: pgtypeconv.Text(ev.RequestID),
	})
}

// List returns recent audit events matching params, most recent first.
func (s *Service) List(ctx context.Context, params auditapi.ListParams) ([]auditapi.Entry, error) {
	limit := params.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	q := auditstore.New(db.Conn(ctx, s.pool))
	rows, err := q.ListAuditEvents(ctx, auditstore.ListAuditEventsParams{
		Actor:       pgtypeconv.Text(params.Actor),
		Subject:     pgtypeconv.Text(params.Subject),
		Action:      pgtypeconv.Text(params.Action),
		Before:      beforeParam(params.Before),
		ResultLimit: int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("audit: list: %w", err)
	}

	entries := make([]auditapi.Entry, 0, len(rows))
	for _, r := range rows {
		var payload map[string]any
		if len(r.Payload) > 0 {
			if err := json.Unmarshal(r.Payload, &payload); err != nil {
				return nil, fmt.Errorf("audit: list: unmarshal payload for %s: %w", pgtypeconv.UUIDString(r.ID), err)
			}
		}
		actorIP := ""
		if r.ActorIp != nil {
			actorIP = r.ActorIp.String()
		}
		entries = append(entries, auditapi.Entry{
			ID:        pgtypeconv.UUIDString(r.ID),
			At:        pgtypeconv.Time(r.At),
			Actor:     r.Actor,
			ActorIP:   actorIP,
			Action:    r.Action,
			Subject:   r.Subject,
			Payload:   payload,
			RequestID: pgtypeconv.TextString(r.RequestID),
		})
	}
	return entries, nil
}

func marshalPayload(payload map[string]any) ([]byte, error) {
	if payload == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(payload)
}

func beforeParam(t time.Time) pgtype.Timestamptz {
	if t.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtypeconv.Timestamptz(t)
}
