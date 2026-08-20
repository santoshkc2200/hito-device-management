// Package auth implements admin login (Argon2id password + TOTP second
// factor), server-side sessions with CSRF double-submit, kiosk bearer-token
// validation, and the HTTP middleware that enforces all of it
// (docs/09-security-privacy-ops.md). It replaces the Phase 0 pass-through
// wholesale, as that stub's own comment promised.
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	authstore "github.com/hito-hospital/hdms/internal/platform/auth/store"
	"github.com/hito-hospital/hdms/internal/platform/clock"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/pgtypeconv"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	ErrInvalidCredentials = errors.New("auth: invalid email, password or code")
	ErrAccountDisabled    = errors.New("auth: account disabled")
	ErrEmailTaken         = errors.New("auth: email already registered")
	ErrSessionInvalid     = errors.New("auth: session invalid or expired")
	ErrKioskInvalid       = errors.New("auth: kiosk token invalid or disabled")
)

// Service backs admin login, sessions and kiosk-token validation against
// Postgres.
type Service struct {
	pool       *db.Pool
	pepper     []byte // HMAC key for session/kiosk token hashing — config.TokenPepper, same pepper credential tokens use
	totpEncKey []byte // AES-256-GCM key for admin_accounts.totp_secret_enc — config.TOTPSecretEncKey
	sessionTTL time.Duration
	clock      clock.Clock
}

// Option customises a Service at construction.
type Option func(*Service)

// WithClock replaces the wall clock this service reads "now" from, so a
// test can drive admin-session and pairing-code expiry with clock.Fake
// instead of sleeping.
func WithClock(c clock.Clock) Option {
	return func(s *Service) { s.clock = c }
}

// New constructs the auth service.
func New(pool *db.Pool, pepper string, totpEncKey []byte, adminSessionTTL time.Duration, opts ...Option) *Service {
	s := &Service{
		pool: pool, pepper: []byte(pepper), totpEncKey: totpEncKey,
		sessionTTL: adminSessionTTL, clock: clock.System{},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// SessionTTL returns the configured admin session TTL, for handlers that
// need to set a cookie's Max-Age to match.
func (s *Service) SessionTTL() time.Duration {
	return s.sessionTTL
}

// CreateAdminAccount inserts a new admin account with a freshly generated
// TOTP secret, returning the plaintext secret and its otpauth:// URL — the
// only time either is ever available again. Used solely by
// `hdms-cli admin bootstrap`; there is no HTTP endpoint for this, matching
// the rest of the system's administrator-only account creation.
func (s *Service) CreateAdminAccount(ctx context.Context, email, fullName, password, role string) (id, totpSecret, otpauthURL string, err error) {
	return s.CreateAdminAccountWithSecret(ctx, email, fullName, password, role, "")
}

// CreateAdminAccountWithSecret inserts a new admin account, optionally with a pre-specified
// base32 TOTP secret (for deterministic test environments).
func (s *Service) CreateAdminAccountWithSecret(ctx context.Context, email, fullName, password, role, customSecret string) (id, totpSecret, otpauthURL string, err error) {
	hash, err := HashPassword(password)
	if err != nil {
		return "", "", "", err
	}
	secret := customSecret
	url := ""
	if secret == "" {
		var err error
		secret, url, err = GenerateTOTPSecret(email)
		if err != nil {
			return "", "", "", err
		}
	} else {
		url = fmt.Sprintf("otpauth://totp/HDMS:%s?secret=%s&issuer=HDMS", email, secret)
	}
	secretEnc, err := encryptSecret(secret, s.totpEncKey)
	if err != nil {
		return "", "", "", fmt.Errorf("auth: encrypt totp secret: %w", err)
	}

	q := authstore.New(db.Conn(ctx, s.pool))
	row, err := q.CreateAdminAccount(ctx, authstore.CreateAdminAccountParams{
		ID:            pgtypeconv.NewUUID(),
		Email:         email,
		FullName:      fullName,
		PasswordHash:  hash,
		TotpSecretEnc: secretEnc,
		Role:          authstore.AdminRole(role),
	})
	if err != nil {
		return "", "", "", translateAccountErr(err)
	}
	return pgtypeconv.UUIDString(row.ID), secret, url, nil
}

// Login verifies email + password + TOTP code and, on success, opens a new
// session. It deliberately returns the same ErrInvalidCredentials for "no
// such account", "wrong password" and "wrong code" so a login form cannot
// be used to enumerate admin emails.
func (s *Service) Login(ctx context.Context, email, password, totpCode string) (sessionToken, csrfToken string, admin AdminIdentity, err error) {
	q := authstore.New(db.Conn(ctx, s.pool))
	account, err := q.GetAdminAccountByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", AdminIdentity{}, ErrInvalidCredentials
		}
		return "", "", AdminIdentity{}, fmt.Errorf("auth: login: %w", err)
	}
	if account.Status != authstore.AdminStatusActive {
		return "", "", AdminIdentity{}, ErrAccountDisabled
	}

	ok, err := VerifyPassword(account.PasswordHash, password)
	if err != nil || !ok {
		return "", "", AdminIdentity{}, ErrInvalidCredentials
	}

	if len(account.TotpSecretEnc) == 0 {
		return "", "", AdminIdentity{}, ErrInvalidCredentials
	}
	secret, err := decryptSecret(account.TotpSecretEnc, s.totpEncKey)
	if err != nil {
		return "", "", AdminIdentity{}, fmt.Errorf("auth: decrypt totp secret: %w", err)
	}
	if !ValidateTOTPCode(secret, totpCode) {
		return "", "", AdminIdentity{}, ErrInvalidCredentials
	}

	admin = AdminIdentity{
		ID:       pgtypeconv.UUIDString(account.ID),
		Email:    account.Email,
		FullName: account.FullName,
		Role:     string(account.Role),
	}

	sessionToken, csrfToken, err = s.createSession(ctx, account.ID)
	if err != nil {
		return "", "", AdminIdentity{}, err
	}
	return sessionToken, csrfToken, admin, nil
}

func (s *Service) createSession(ctx context.Context, adminID pgtype.UUID) (sessionToken, csrfToken string, err error) {
	plainToken, err := randomToken(32)
	if err != nil {
		return "", "", err
	}
	csrfToken, err = randomToken(24)
	if err != nil {
		return "", "", err
	}

	q := authstore.New(db.Conn(ctx, s.pool))
	_, err = q.CreateSession(ctx, authstore.CreateSessionParams{
		ID:               pgtypeconv.NewUUID(),
		AdminID:          adminID,
		SessionTokenHash: hashToken(plainToken, s.pepper),
		CsrfToken:        csrfToken,
		ExpiresAt:        pgtypeconv.Timestamptz(s.clock.Now().Add(s.sessionTTL)),
	})
	if err != nil {
		return "", "", fmt.Errorf("auth: create session: %w", err)
	}
	return plainToken, csrfToken, nil
}

// ValidatedSession is what Middleware needs from a valid session cookie: the
// identity to attach to the request, and the CSRF token to compare against
// the X-CSRF-Token header on mutating requests.
type ValidatedSession struct {
	Admin     AdminIdentity
	CSRFToken string
}

// ValidateSession looks up plainToken, rejects it if expired or the owning
// account is disabled, and otherwise slides its expiry forward by the
// configured TTL (docs/09's "12-hour expiry with sliding renewal").
func (s *Service) ValidateSession(ctx context.Context, plainToken string) (ValidatedSession, error) {
	q := authstore.New(db.Conn(ctx, s.pool))
	row, err := q.GetSessionByTokenHash(ctx, hashToken(plainToken, s.pepper))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ValidatedSession{}, ErrSessionInvalid
		}
		return ValidatedSession{}, fmt.Errorf("auth: validate session: %w", err)
	}
	if row.AdminStatus != authstore.AdminStatusActive {
		return ValidatedSession{}, ErrAccountDisabled
	}
	if s.clock.Now().After(pgtypeconv.Time(row.ExpiresAt)) {
		return ValidatedSession{}, ErrSessionInvalid
	}

	if _, err := q.RenewSession(ctx, authstore.RenewSessionParams{
		ID:        row.ID,
		ExpiresAt: pgtypeconv.Timestamptz(s.clock.Now().Add(s.sessionTTL)),
	}); err != nil {
		return ValidatedSession{}, fmt.Errorf("auth: renew session: %w", err)
	}

	return ValidatedSession{
		Admin: AdminIdentity{
			ID:       pgtypeconv.UUIDString(row.AdminID),
			Email:    row.Email,
			FullName: row.FullName,
			Role:     string(row.Role),
		},
		CSRFToken: row.CsrfToken,
	}, nil
}

// RevokeSession deletes plainToken's session row (logout).
func (s *Service) RevokeSession(ctx context.Context, plainToken string) error {
	q := authstore.New(db.Conn(ctx, s.pool))
	if err := q.DeleteSessionByTokenHash(ctx, hashToken(plainToken, s.pepper)); err != nil {
		return fmt.Errorf("auth: revoke session: %w", err)
	}
	return nil
}

// ValidateKioskToken looks up a kiosk bearer token, rejects a disabled
// kiosk, and records the touch. Unused by any Phase 1 route — see
// KioskIdentity's doc comment.
func (s *Service) ValidateKioskToken(ctx context.Context, plainToken string) (KioskIdentity, error) {
	q := authstore.New(db.Conn(ctx, s.pool))
	row, err := q.GetKioskByTokenHash(ctx, hashToken(plainToken, s.pepper))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return KioskIdentity{}, ErrKioskInvalid
		}
		return KioskIdentity{}, fmt.Errorf("auth: validate kiosk token: %w", err)
	}
	if row.Status != authstore.KioskStatusActive {
		return KioskIdentity{}, ErrKioskInvalid
	}
	if err := q.UpdateKioskLastSeen(ctx, row.ID); err != nil {
		return KioskIdentity{}, fmt.Errorf("auth: update kiosk last seen: %w", err)
	}
	return KioskIdentity{ID: pgtypeconv.UUIDString(row.ID), Name: row.Name}, nil
}

// randomToken returns a URL-safe base64 encoding of n random bytes.
func randomToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("auth: generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// hashToken computes HMAC-SHA256(token, pepper) — the same scheme
// credential tokens use, so session and kiosk tokens are equally
// unreadable from a database backup alone.
func hashToken(token string, pepper []byte) []byte {
	mac := hmac.New(sha256.New, pepper)
	mac.Write([]byte(token))
	return mac.Sum(nil)
}

func translateAccountErr(err error) error {
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == "23505" {
		return ErrEmailTaken
	}
	return err
}
