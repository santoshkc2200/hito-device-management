// Package auditapi is the public surface of the audit module. Only this
// package may be imported by other modules; everything under
// internal/modules/audit/internal is unreachable outside the module by
// Go's own visibility rules.
package auditapi

import (
	"context"
	"time"
)

// Event is one row destined for the append-only audit_events table
// (docs/03-domain-model.md, NFR-15). Every mutating operation in identity,
// catalog and credentials records one of these in the same transaction as
// its domain change.
type Event struct {
	Actor     string // 'admin:<id>' | 'kiosk:<id>' | 'import' | 'system'
	ActorIP   string // optional; "" if unknown
	Action    string // 'user.created', 'credential.reissued', …
	Subject   string // 'user:<uuid>', 'device:<uuid>', …
	Payload   map[string]any
	RequestID string // optional; "" if unknown
}

// Entry is a stored audit event, as returned by List.
type Entry struct {
	ID        string
	At        time.Time
	Actor     string
	ActorIP   string
	Action    string
	Subject   string
	Payload   map[string]any
	RequestID string
}

// ListParams filters List; a zero-value field matches everything.
type ListParams struct {
	Actor    string
	Subject  string
	Action   string
	From     time.Time
	To       time.Time
	CursorAt time.Time
	CursorID string
	Limit    int
}

// Recorder is what every other module depends on to write an audit event.
// It is deliberately narrow — write-only — matching audit's role as a sink
// nothing downstream of it needs to query synchronously.
type Recorder interface {
	Record(ctx context.Context, ev Event) error
}

// Service is the full audit module API, adding the read side the admin
// console's audit log screen needs.
type Service interface {
	Recorder
	List(ctx context.Context, params ListParams) ([]Entry, error)
	StreamForExport(ctx context.Context, params ListParams) ([]Entry, error)
}
