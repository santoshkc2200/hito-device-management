// Package staffauth owns the staff authentication realm: staff accounts,
// their external identity links, their sessions, and the Entra OIDC client.
//
// It deliberately knows nothing about users, devices or credentials — every
// method takes and returns a user ID. Resolving an employee number to a user,
// provisioning a user, and minting a QR are orchestrated one layer up, in
// apiserver, the same way RegisterWithCard already orchestrates identity and
// credentials. That is what keeps this package a platform concern rather than
// a second cross-module orchestrator (ADR-0009).
package staffauth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/auth"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/pgtypeconv"
	staffauthstore "github.com/hito-hospital/hdms/internal/platform/staffauth/store"
	"github.com/jackc/pgx/v5"
)

// MaxFailedAttempts and LockoutWindow mirror the administrator policy in
// docs/09-security-privacy-ops.md.
const (
	MaxFailedAttempts = 5
	LockoutWindow     = 15 * time.Minute
)

var (
	ErrAccountNotFound    = errors.New("staffauth: staff account not found")
	ErrAccountLocked      = errors.New("staffauth: account is temporarily locked")
	ErrInvalidCredentials = errors.New("staffauth: invalid credentials")
	ErrNoPasswordSet      = errors.New("staffauth: this account has no password")
	ErrSessionInvalid     = errors.New("staffauth: session invalid or expired")
	ErrWeakPassword       = errors.New("staffauth: password must be at least 12 characters")
)

// Account is the staff principal as the rest of the system sees it. The
// password hash never leaves this package.
type Account struct {
	ID                 string
	UserID             string
	HasPassword        bool
	MustChangePassword bool
	ProfileComplete    bool
}

type Service struct {
	pool       *db.Pool
	sessionTTL time.Duration
}

func New(pool *db.Pool, sessionTTL time.Duration) *Service {
	return &Service{pool: pool, sessionTTL: sessionTTL}
}

func (s *Service) SessionTTL() time.Duration { return s.sessionTTL }

func toAccount(row staffauthstore.StaffAccount) Account {
	return Account{
		ID:                 pgtypeconv.UUIDString(row.ID),
		UserID:             pgtypeconv.UUIDString(row.UserID),
		HasPassword:        row.PasswordHash.Valid && row.PasswordHash.String != "",
		MustChangePassword: row.MustChangePassword,
		ProfileComplete:    row.ProfileComplete,
	}
}

// EnsureAccount returns the staff account for a user, creating it if this is
// the first time that user has signed in. It is idempotent so a racing second
// callback cannot produce two accounts for one user — the UNIQUE constraint
// on user_id is what actually enforces that.
func (s *Service) EnsureAccount(ctx context.Context, userID, createdBy string, profileComplete bool) (Account, error) {
	uid, err := pgtypeconv.UUID(userID)
	if err != nil {
		return Account{}, fmt.Errorf("staffauth: invalid user id: %w", err)
	}
	q := staffauthstore.New(db.Conn(ctx, s.pool))

	row, err := q.GetStaffAccountByUserID(ctx, uid)
	if err == nil {
		return toAccount(row), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Account{}, fmt.Errorf("staffauth: look up account: %w", err)
	}

	created, err := q.CreateStaffAccount(ctx, staffauthstore.CreateStaffAccountParams{
		ID:                 pgtypeconv.NewUUID(),
		UserID:             uid,
		PasswordHash:       pgtypeconv.Text(""),
		MustChangePassword: false,
		ProfileComplete:    profileComplete,
		CreatedBy:          createdBy,
	})
	if err != nil {
		// A concurrent callback won the race; its row is the one that counts.
		if again, getErr := q.GetStaffAccountByUserID(ctx, uid); getErr == nil {
			return toAccount(again), nil
		}
		return Account{}, fmt.Errorf("staffauth: create account: %w", err)
	}
	return toAccount(created), nil
}

func (s *Service) AccountForUser(ctx context.Context, userID string) (Account, error) {
	uid, err := pgtypeconv.UUID(userID)
	if err != nil {
		return Account{}, fmt.Errorf("staffauth: invalid user id: %w", err)
	}
	q := staffauthstore.New(db.Conn(ctx, s.pool))
	row, err := q.GetStaffAccountByUserID(ctx, uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrAccountNotFound
	}
	if err != nil {
		return Account{}, fmt.Errorf("staffauth: look up account: %w", err)
	}
	return toAccount(row), nil
}

func (s *Service) AccountForIdentity(ctx context.Context, provider, subject string) (Account, error) {
	q := staffauthstore.New(db.Conn(ctx, s.pool))
	identityRow, err := q.GetStaffIdentity(ctx, staffauthstore.GetStaffIdentityParams{
		Provider: provider,
		Subject:  subject,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrAccountNotFound
	}
	if err != nil {
		return Account{}, fmt.Errorf("staffauth: look up identity: %w", err)
	}

	acctRow, err := q.GetStaffAccountByID(ctx, identityRow.StaffAccountID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrAccountNotFound
	}
	if err != nil {
		return Account{}, fmt.Errorf("staffauth: look up account for identity: %w", err)
	}
	return toAccount(acctRow), nil
}

func (s *Service) LinkIdentity(ctx context.Context, accountID, provider, subject, tenantID, email string) error {
	aid, err := pgtypeconv.UUID(accountID)
	if err != nil {
		return fmt.Errorf("staffauth: invalid account id: %w", err)
	}
	q := staffauthstore.New(db.Conn(ctx, s.pool))
	if _, err := q.LinkStaffIdentity(ctx, staffauthstore.LinkStaffIdentityParams{
		ID:             pgtypeconv.NewUUID(),
		StaffAccountID: aid,
		Provider:       provider,
		Subject:        subject,
		TenantID:       tenantID,
		EmailAtLink:    pgtypeconv.Text(email),
	}); err != nil {
		return fmt.Errorf("staffauth: link identity: %w", err)
	}
	return nil
}

func (s *Service) IdentitiesForAccount(ctx context.Context, accountID string) ([]staffauthstore.StaffIdentity, error) {
	aid, err := pgtypeconv.UUID(accountID)
	if err != nil {
		return nil, fmt.Errorf("staffauth: invalid account id: %w", err)
	}
	q := staffauthstore.New(db.Conn(ctx, s.pool))
	return q.ListStaffIdentitiesForAccount(ctx, aid)
}

func (s *Service) getByID(ctx context.Context, accountID string) (staffauthstore.StaffAccount, error) {
	aid, err := pgtypeconv.UUID(accountID)
	if err != nil {
		return staffauthstore.StaffAccount{}, fmt.Errorf("staffauth: invalid account id: %w", err)
	}
	q := staffauthstore.New(db.Conn(ctx, s.pool))
	row, err := q.GetStaffAccountByID(ctx, aid)
	if errors.Is(err, pgx.ErrNoRows) {
		return staffauthstore.StaffAccount{}, ErrAccountNotFound
	}
	if err != nil {
		return staffauthstore.StaffAccount{}, fmt.Errorf("staffauth: look up account: %w", err)
	}
	return row, nil
}

// SetPassword is the administrator path (a reset) and the provisioning path.
// mustChange forces a change at next sign-in, which is what a temporary
// password means.
func (s *Service) SetPassword(ctx context.Context, accountID, plaintext string, mustChange bool) error {
	if len([]rune(plaintext)) < 12 {
		return ErrWeakPassword
	}
	hash, err := auth.HashPassword(plaintext)
	if err != nil {
		return fmt.Errorf("staffauth: hash password: %w", err)
	}
	aid, err := pgtypeconv.UUID(accountID)
	if err != nil {
		return fmt.Errorf("staffauth: invalid account id: %w", err)
	}
	q := staffauthstore.New(db.Conn(ctx, s.pool))
	if _, err := q.SetStaffPassword(ctx, staffauthstore.SetStaffPasswordParams{
		ID:                 aid,
		PasswordHash:       pgtypeconv.Text(hash),
		MustChangePassword: mustChange,
	}); err != nil {
		return fmt.Errorf("staffauth: set password: %w", err)
	}
	return nil
}

// VerifyPassword checks a password and maintains the lockout counters. The
// caller must not distinguish its errors to the client: an unknown employee
// number and a wrong password answer identically.
func (s *Service) VerifyPassword(ctx context.Context, accountID, plaintext string) error {
	row, err := s.getByID(ctx, accountID)
	if err != nil {
		return err
	}
	if row.LockedUntil.Valid && row.LockedUntil.Time.After(time.Now()) {
		return ErrAccountLocked
	}
	if !row.PasswordHash.Valid || row.PasswordHash.String == "" {
		return ErrNoPasswordSet
	}

	q := staffauthstore.New(db.Conn(ctx, s.pool))
	ok, err := auth.VerifyPassword(row.PasswordHash.String, plaintext)
	if err != nil || !ok {
		lockout := LockoutWindow
		if _, failErr := q.RecordStaffLoginFailure(ctx, staffauthstore.RecordStaffLoginFailureParams{
			ID:             row.ID,
			FailedAttempts: int32(MaxFailedAttempts),
			Column3:        pgtypeconv.Interval(&lockout),
		}); failErr != nil {
			return fmt.Errorf("staffauth: record failure: %w", failErr)
		}
		return ErrInvalidCredentials
	}
	if err := q.RecordStaffLoginSuccess(ctx, row.ID); err != nil {
		return fmt.Errorf("staffauth: record success: %w", err)
	}
	return nil
}

// ChangeOwnPassword is the self-service path: it requires the current
// password and clears must_change_password.
func (s *Service) ChangeOwnPassword(ctx context.Context, accountID, current, next string) error {
	if err := s.VerifyPassword(ctx, accountID, current); err != nil {
		return err
	}
	return s.SetPassword(ctx, accountID, next, false)
}

func (s *Service) MarkProfileComplete(ctx context.Context, accountID string) (Account, error) {
	aid, err := pgtypeconv.UUID(accountID)
	if err != nil {
		return Account{}, fmt.Errorf("staffauth: invalid account id: %w", err)
	}
	q := staffauthstore.New(db.Conn(ctx, s.pool))
	row, err := q.MarkStaffProfileComplete(ctx, aid)
	if err != nil {
		return Account{}, fmt.Errorf("staffauth: mark profile complete: %w", err)
	}
	return toAccount(row), nil
}
