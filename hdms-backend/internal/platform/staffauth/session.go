package staffauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/pgtypeconv"
	staffauthstore "github.com/hito-hospital/hdms/internal/platform/staffauth/store"
	"github.com/jackc/pgx/v5"
)

// ValidatedSession is what the middleware attaches to the request context.
type ValidatedSession struct {
	Account   Account
	CSRFToken string
}

func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("staffauth: read random: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// StartSession mints a session token and its CSRF partner. Only the hash of
// the session token is stored, exactly as admin_sessions does.
func (s *Service) StartSession(ctx context.Context, accountID string) (string, string, error) {
	sessionToken, err := randomToken()
	if err != nil {
		return "", "", err
	}
	csrfToken, err := randomToken()
	if err != nil {
		return "", "", err
	}
	aid, err := pgtypeconv.UUID(accountID)
	if err != nil {
		return "", "", fmt.Errorf("staffauth: invalid account id: %w", err)
	}

	q := staffauthstore.New(db.Conn(ctx, s.pool))
	if _, err := q.CreateStaffSession(ctx, staffauthstore.CreateStaffSessionParams{
		ID:               pgtypeconv.NewUUID(),
		StaffAccountID:   aid,
		SessionTokenHash: hashToken(sessionToken),
		CsrfToken:        csrfToken,
		ExpiresAt:        pgtypeconv.Timestamptz(time.Now().Add(s.sessionTTL)),
	}); err != nil {
		return "", "", fmt.Errorf("staffauth: create session: %w", err)
	}
	return sessionToken, csrfToken, nil
}

// ValidateSession resolves a session token, slides its expiry, and returns
// the account. Expiry is checked in Go rather than SQL so an expired row
// produces ErrSessionInvalid rather than a confusing not-found.
func (s *Service) ValidateSession(ctx context.Context, sessionToken string) (ValidatedSession, error) {
	if sessionToken == "" {
		return ValidatedSession{}, ErrSessionInvalid
	}
	q := staffauthstore.New(db.Conn(ctx, s.pool))
	row, err := q.GetStaffSessionByTokenHash(ctx, hashToken(sessionToken))
	if errors.Is(err, pgx.ErrNoRows) {
		return ValidatedSession{}, ErrSessionInvalid
	}
	if err != nil {
		return ValidatedSession{}, fmt.Errorf("staffauth: look up session: %w", err)
	}
	if !row.ExpiresAt.Valid || row.ExpiresAt.Time.Before(time.Now()) {
		return ValidatedSession{}, ErrSessionInvalid
	}

	account, err := q.GetStaffAccountByID(ctx, row.StaffAccountID)
	if err != nil {
		return ValidatedSession{}, fmt.Errorf("staffauth: load account for session: %w", err)
	}
	if err := q.SlideStaffSession(ctx, staffauthstore.SlideStaffSessionParams{
		ID:        row.ID,
		ExpiresAt: pgtypeconv.Timestamptz(time.Now().Add(s.sessionTTL)),
	}); err != nil {
		return ValidatedSession{}, fmt.Errorf("staffauth: slide session: %w", err)
	}

	return ValidatedSession{Account: toAccount(account), CSRFToken: row.CsrfToken}, nil
}

func (s *Service) RevokeSession(ctx context.Context, sessionToken string) error {
	q := staffauthstore.New(db.Conn(ctx, s.pool))
	if err := q.DeleteStaffSessionByTokenHash(ctx, hashToken(sessionToken)); err != nil {
		return fmt.Errorf("staffauth: revoke session: %w", err)
	}
	return nil
}

// RevokeAllSessions is what a password reset calls: changing a password must
// end every session that the old one opened.
func (s *Service) RevokeAllSessions(ctx context.Context, accountID string) error {
	aid, err := pgtypeconv.UUID(accountID)
	if err != nil {
		return fmt.Errorf("staffauth: invalid account id: %w", err)
	}
	q := staffauthstore.New(db.Conn(ctx, s.pool))
	if err := q.DeleteStaffSessionsForAccount(ctx, aid); err != nil {
		return fmt.Errorf("staffauth: revoke sessions: %w", err)
	}
	return nil
}
