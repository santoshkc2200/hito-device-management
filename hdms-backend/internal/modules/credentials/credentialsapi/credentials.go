// Package credentialsapi is the public surface of the credentials module.
// Only this package may be imported by other modules; everything under
// internal/modules/credentials/internal is unreachable outside the module
// by Go's own visibility rules.
package credentialsapi

import (
	"context"
	"errors"
	"time"
)

// Kind mirrors the credential_kind Postgres enum.
type Kind string

const (
	KindQR      Kind = "qr"
	KindCode128 Kind = "code128"
	KindNFC     Kind = "nfc"
	KindRFID    Kind = "rfid"
	KindManual  Kind = "manual"
)

// SubjectType mirrors the subject_type Postgres enum.
type SubjectType string

const (
	SubjectUser   SubjectType = "user"
	SubjectDevice SubjectType = "device"
)

// Status mirrors the credential_status Postgres enum.
type Status string

const (
	StatusActive  Status = "active"
	StatusRevoked Status = "revoked"
	StatusLost    Status = "lost"
)

// RefType is Resolve's subject discriminator. Unbound and Unknown are
// resolution outcomes, not enum values stored anywhere — a scanned token
// that matches no row at all is Unknown; a real, unbound blank card is
// Unbound (INV-12), which is a successful resolution, not an error.
type RefType string

const (
	RefUser    RefType = "user"
	RefDevice  RefType = "device"
	RefUnbound RefType = "unbound"
)

var (
	// ErrCredentialUnknown is returned by Resolve only when the token
	// matches no credential at all — never for an unbound, revoked, or
	// lost one, all of which are successful resolutions with the status
	// to match.
	ErrCredentialUnknown = errors.New("credentials: token does not match any credential")

	ErrCredentialNotFound     = errors.New("credentials: credential not found")
	ErrCredentialNotUnbound   = errors.New("credentials: credential is not an unbound, active card")
	ErrCredentialNotActive    = errors.New("credentials: credential is not active")
	ErrNotDeviceCredential    = errors.New("credentials: only device credentials store a recoverable token")
	ErrKindNotIssuableInV1    = errors.New("credentials: nfc and rfid credentials are not issued until Phase 6")
	ErrManualTokenRequired    = errors.New("credentials: a manual credential requires an explicit token value")
	ErrTokenAlreadyRegistered = errors.New("credentials: that token is already registered to a credential")
	ErrTokenNotRecoverable    = errors.New("credentials: this credential predates reversible storage")
)

// SubjectRef is what Resolve returns for a scanned token: which subject it
// names (or Unbound), and the credential's own status, since a revoked or
// lost credential still resolves — it just refuses to act as a valid
// subject (INV-4).
type SubjectRef struct {
	Type             RefType
	SubjectID        string // "" when Type is Unbound
	CredentialID     string
	CredentialStatus Status
	Kind             Kind

	// RevokedAt is when the credential stopped being valid, nil while it
	// still is. The kiosk's "this card was replaced on …" message is the
	// only caller: a scan of a dead card is worth explaining, and the date
	// is what tells someone whether it was replaced today or last year.
	RevokedAt *time.Time
}

// Credential is the credentials module's read model for one credential
// row. Token is never populated except by Issue/Reissue/Reprint, and only
// in their return value — it is never retrievable again afterward.
type Credential struct {
	ID            string
	SubjectType   SubjectType
	SubjectID     string // "" if unbound
	Kind          Kind
	TokenPreview  string
	Label         string
	Status        Status
	IssueSeq      int
	ReplacesID    string
	IssuedAt      time.Time
	IssuedBy      string
	RevokedAt     *time.Time
	RevokedBy     string
	RevokedReason string
	PrintedCount  int
	LastPrintedAt *time.Time
}

// IssuedCredential is a Credential together with the plaintext token,
// returned exactly once at the moment of issuance, reprint, or reissue.
type IssuedCredential struct {
	Credential
	Token string
}

// Event is one row from a credential's issuance history.
type Event struct {
	At     time.Time
	Kind   string // 'issued' | 'reprinted' | 'revoked' | 'reissued' | 'bound'
	Actor  string
	Reason string
}

// IssueParams mints a new credential. For SubjectType user with no
// SubjectID, this creates unbound blank card stock (INV-12). ManualToken
// is required, and used verbatim, only when Kind is KindManual.
type IssueParams struct {
	SubjectType SubjectType
	SubjectID   string // "" = unbound blank stock
	Kind        Kind
	Label       string
	ManualToken string
	IssuedBy    string
}

// Service is the credentials module's public API.
type Service interface {
	// Issue mints a new credential and returns its plaintext token — the
	// only time that token is ever retrievable.
	Issue(ctx context.Context, params IssueParams) (IssuedCredential, error)

	// IssueBlankBatch mints count unbound, active user credentials as
	// pre-printed card stock (docs/05's "blank card stock").
	IssueBlankBatch(ctx context.Context, count int, kind Kind, issuedBy string) ([]IssuedCredential, error)

	// Resolve looks up a scanned or typed token. It returns
	// ErrCredentialUnknown only if the token matches nothing; an unbound,
	// revoked, or lost credential is a successful resolution carrying that
	// status, per INV-4 and INV-12.
	Resolve(ctx context.Context, token string) (SubjectRef, error)

	// Bind attaches an unbound, active credential to a subject — the
	// operation behind "scan a blank card from the drawer" at borrower
	// registration (FR-41, FR-59).
	Bind(ctx context.Context, credentialID, subjectID, actor string) (Credential, error)

	// Reprint recovers and re-returns the plaintext token for a device
	// credential (the only kind stored reversibly) and increments
	// printed_count. It fails with ErrNotDeviceCredential for a user
	// credential — those must be reissued instead.
	Reprint(ctx context.Context, credentialID, actor string) (IssuedCredential, error)

	// Reveal returns the plaintext token of an active credential of either
	// subject type. It does not count as a print and does not mint anything.
	Reveal(ctx context.Context, credentialID, actor string) (IssuedCredential, error)

	// Revoke kills a token permanently with no replacement minted.
	Revoke(ctx context.Context, credentialID, reason, actor string) (Credential, error)

	// Reissue is for a lost card: the old credential becomes 'lost', and a
	// new one is minted in the same transaction, linked by replaces_id.
	// The subject's identity and loan history are untouched.
	Reissue(ctx context.Context, credentialID, reason, actor, manualToken string) (IssuedCredential, error)

	// ListBySubject returns every credential ever issued to a subject,
	// most recent first.
	ListBySubject(ctx context.Context, subjectType SubjectType, subjectID string) ([]Credential, error)

	// ListEvents returns one credential's issuance history.
	ListEvents(ctx context.Context, credentialID string) ([]Event, error)

	// CountUnbound backs the "blank card stock running low" dashboard
	// warning.
	CountUnbound(ctx context.Context) (int, error)
}
