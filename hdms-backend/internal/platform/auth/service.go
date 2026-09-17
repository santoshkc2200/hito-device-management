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
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/audit/auditapi"
	authstore "github.com/hito-hospital/hdms/internal/platform/auth/store"
	"github.com/hito-hospital/hdms/internal/platform/clock"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/httpx"
	"github.com/hito-hospital/hdms/internal/platform/pgtypeconv"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	MaxFailedAttempts = 5
	LockoutDuration   = 15 * time.Minute
	FailureWindow     = 15 * time.Minute
	RecoveryCodeCount = 8
)

var (
	ErrInvalidCredentials = errors.New("auth: invalid email, password or code")
	ErrAccountDisabled    = errors.New("auth: account disabled")
	ErrAccountLocked      = errors.New("auth: account locked due to repeated failed logins")
	ErrEmailTaken         = errors.New("auth: email already registered")
	ErrSessionInvalid     = errors.New("auth: session invalid or expired")
	ErrKioskInvalid       = errors.New("auth: kiosk token invalid or disabled")
	ErrKioskNotFound      = errors.New("auth: kiosk not found")
	ErrPairingCodeInvalid = errors.New("auth: pairing code is invalid or expired")
	ErrAdminNotFound      = errors.New("auth: admin account not found")
	ErrNoPendingTotp      = errors.New("auth: no pending totp re-enrolment")
	ErrInvalidPassword    = errors.New("auth: invalid current password")
	ErrPasswordTooShort   = errors.New("auth: password must be at least 12 characters")
)

// Service backs admin login, sessions and kiosk-token validation against
// Postgres.
type Service struct {
	pool       *db.Pool
	pepper     []byte // HMAC key for session/kiosk token hashing — config.TokenPepper, same pepper credential tokens use
	totpEncKey []byte // AES-256-GCM key for admin_accounts.totp_secret_enc — config.TOTPSecretEncKey
	sessionTTL time.Duration
	clock      clock.Clock
	audit      auditapi.Recorder
	loginMu    sync.Mutex // serialises recovery code redemptions / login checks
}

// Option customises a Service at construction.
type Option func(*Service)

// WithClock replaces the wall clock this service reads "now" from, so a
// test can drive admin-session and pairing-code expiry with clock.Fake
// instead of sleeping.
func WithClock(c clock.Clock) Option {
	return func(s *Service) { s.clock = c }
}

// WithAudit attaches an audit recorder to the service so that security
// and authorization events (such as 403 role check failures) are audited.
func WithAudit(a auditapi.Recorder) Option {
	return func(s *Service) { s.audit = a }
}

func (s *Service) recordAudit(ctx context.Context, r *http.Request, actor, action, subject string, payload map[string]any) {
	if s.audit == nil {
		return
	}
	var ip string
	var reqID string
	if r != nil {
		ip = clientIP(r)
		reqID = httpx.RequestID(r.Context())
	}
	_ = s.audit.Record(ctx, auditapi.Event{
		Actor:     actor,
		ActorIP:   ip,
		Action:    action,
		Subject:   subject,
		Payload:   payload,
		RequestID: reqID,
	})
}

func (s *Service) recordRoleDenied(ctx context.Context, r *http.Request, admin AdminIdentity, minRole string) {
	s.recordAudit(ctx, r, "admin:"+admin.ID, "auth.role_denied", "admin:"+admin.ID, map[string]any{
		"method":        r.Method,
		"path":          r.URL.Path,
		"role":          admin.Role,
		"required_role": minRole,
	})
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
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

// GenerateRecoveryCodes creates n human-readable single-use recovery codes.
func GenerateRecoveryCodes(n int) ([]string, error) {
	codes := make([]string, n)
	for i := 0; i < n; i++ {
		b := make([]byte, 5)
		if _, err := rand.Read(b); err != nil {
			return nil, fmt.Errorf("auth: generate recovery code: %w", err)
		}
		hexStr := strings.ToUpper(hex.EncodeToString(b))
		codes[i] = hexStr[:4] + "-" + hexStr[4:8]
	}
	return codes, nil
}

// CreateAdminAccount inserts a new admin account with a freshly generated
// TOTP secret, returning the plaintext secret and its otpauth:// URL — the
// only time either is ever available again. Used by bootstrap and API.
func (s *Service) CreateAdminAccount(ctx context.Context, email, fullName, password, role string) (id, totpSecret, otpauthURL string, err error) {
	return s.CreateAdminAccountWithSecret(ctx, email, fullName, password, role, "")
}

// CreateAdminAccountWithSecret inserts a new admin account, optionally with a pre-specified
// base32 TOTP secret (for deterministic test environments).
func (s *Service) CreateAdminAccountWithSecret(ctx context.Context, email, fullName, password, role, customSecret string) (id, totpSecret, otpauthURL string, err error) {
	if len(password) < 12 {
		return "", "", "", ErrPasswordTooShort
	}
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

// CreateAdminFull inserts a new admin account and generates initial recovery codes.
func (s *Service) CreateAdminFull(ctx context.Context, actorID, email, fullName, password, role string) (admin AdminIdentity, totpSecret, otpauthURL string, recoveryCodes []string, err error) {
	if len(password) < 12 {
		return AdminIdentity{}, "", "", nil, ErrPasswordTooShort
	}
	id, secret, url, err := s.CreateAdminAccount(ctx, email, fullName, password, role)
	if err != nil {
		return AdminIdentity{}, "", "", nil, err
	}

	codes, err := s.RegenerateRecoveryCodes(ctx, id)
	if err != nil {
		return AdminIdentity{}, "", "", nil, fmt.Errorf("auth: generate recovery codes: %w", err)
	}

	admin = AdminIdentity{
		ID:       id,
		Email:    email,
		FullName: fullName,
		Role:     role,
		Status:   "active",
		Locale:   "ja",
	}

	s.recordAudit(ctx, nil, actorID, "admin.created", "admin:"+id, map[string]any{
		"email": email,
		"role":  role,
	})

	return admin, secret, url, codes, nil
}

// Login verifies email + password + (TOTP code or recovery code) and, on success, opens a new
// session. It handles lockout after repeated failures.
func (s *Service) Login(ctx context.Context, email, password, totpCode string) (sessionToken, csrfToken string, admin AdminIdentity, err error) {
	return s.LoginWithRecovery(ctx, email, password, totpCode, "")
}

// LoginWithRecovery supports logging in either with TOTP code or with a single-use recovery code.
func (s *Service) LoginWithRecovery(ctx context.Context, email, password, totpCode, recoveryCode string) (sessionToken, csrfToken string, admin AdminIdentity, err error) {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()

	q := authstore.New(db.Conn(ctx, s.pool))
	account, err := q.GetAdminAccountByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			s.recordAudit(ctx, nil, "system", "auth.login_failed", "admin:unknown", map[string]any{
				"email": email,
			})
			return "", "", AdminIdentity{}, ErrInvalidCredentials
		}
		return "", "", AdminIdentity{}, fmt.Errorf("auth: login: %w", err)
	}

	adminIDStr := pgtypeconv.UUIDString(account.ID)

	// Check if locked
	now := s.clock.Now()
	if account.LockedUntil.Valid && now.Before(pgtypeconv.Time(account.LockedUntil)) {
		s.recordAudit(ctx, nil, "admin:"+adminIDStr, "auth.login_blocked_locked", "admin:"+adminIDStr, map[string]any{
			"locked_until": pgtypeconv.Time(account.LockedUntil),
		})
		return "", "", AdminIdentity{}, ErrAccountLocked
	}

	if account.Status != authstore.AdminStatusActive {
		return "", "", AdminIdentity{}, ErrAccountDisabled
	}

	// Verify password
	ok, err := VerifyPassword(account.PasswordHash, password)
	if err != nil || !ok {
		s.handleFailedLogin(ctx, account.ID, account.FailedAttempts, account.LastFailureAt, "password_mismatch")
		return "", "", AdminIdentity{}, ErrInvalidCredentials
	}

	// 2FA check: either recovery code or TOTP
	var usedRecoveryCodeID pgtype.UUID
	isRecovery := strings.TrimSpace(recoveryCode) != ""

	if isRecovery {
		// Find matching unused recovery code
		unusedCodes, err := q.ListUnusedRecoveryCodesByAdminID(ctx, account.ID)
		if err != nil {
			return "", "", AdminIdentity{}, fmt.Errorf("auth: list recovery codes: %w", err)
		}
		var matchedID pgtype.UUID
		for _, rc := range unusedCodes {
			if match, _ := VerifyPassword(rc.CodeHash, strings.TrimSpace(recoveryCode)); match {
				matchedID = rc.ID
				break
			}
		}
		if !matchedID.Valid {
			s.handleFailedLogin(ctx, account.ID, account.FailedAttempts, account.LastFailureAt, "recovery_code_no_match")
			return "", "", AdminIdentity{}, ErrInvalidCredentials
		}

		// Atomically consume recovery code
		marked, err := q.MarkRecoveryCodeUsed(ctx, matchedID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// Code was already used concurrently
				s.handleFailedLogin(ctx, account.ID, account.FailedAttempts, account.LastFailureAt, "recovery_code_already_used")
				return "", "", AdminIdentity{}, ErrInvalidCredentials
			}
			return "", "", AdminIdentity{}, fmt.Errorf("auth: consume recovery code: %w", err)
		}
		usedRecoveryCodeID = marked.ID
	} else {
		if strings.TrimSpace(totpCode) == "" {
			s.handleFailedLogin(ctx, account.ID, account.FailedAttempts, account.LastFailureAt, "totp_code_missing")
			return "", "", AdminIdentity{}, ErrInvalidCredentials
		}
		if len(account.TotpSecretEnc) == 0 {
			s.handleFailedLogin(ctx, account.ID, account.FailedAttempts, account.LastFailureAt, "totp_not_enrolled")
			return "", "", AdminIdentity{}, ErrInvalidCredentials
		}
		secret, err := decryptSecret(account.TotpSecretEnc, s.totpEncKey)
		if err != nil {
			return "", "", AdminIdentity{}, fmt.Errorf("auth: decrypt totp secret: %w", err)
		}
		if !ValidateTOTPCode(secret, totpCode) {
			s.handleFailedLogin(ctx, account.ID, account.FailedAttempts, account.LastFailureAt, "totp_code_invalid")
			return "", "", AdminIdentity{}, ErrInvalidCredentials
		}
	}

	// Login succeeded: reset failure counters and set last login
	_ = q.RecordLoginSuccess(ctx, account.ID)

	var lockedUntil *time.Time
	if account.LockedUntil.Valid {
		t := pgtypeconv.Time(account.LockedUntil)
		lockedUntil = &t
	}
	var lastLoginAt *time.Time
	if account.LastLoginAt.Valid {
		t := pgtypeconv.Time(account.LastLoginAt)
		lastLoginAt = &t
	}

	admin = AdminIdentity{
		ID:                 adminIDStr,
		Email:              account.Email,
		FullName:           account.FullName,
		Role:               string(account.Role),
		Status:             string(account.Status),
		MustChangePassword: account.MustChangePassword,
		MustReenrolTotp:    account.MustReenrolTotp,
		LockedUntil:        lockedUntil,
		LastLoginAt:        lastLoginAt,
		Locale:             account.Locale,
	}

	if isRecovery {
		s.recordAudit(ctx, nil, "admin:"+adminIDStr, "auth.recovery_code_used", "admin:"+adminIDStr, map[string]any{
			"code_id": pgtypeconv.UUIDString(usedRecoveryCodeID),
		})
	}

	s.recordAudit(ctx, nil, "admin:"+adminIDStr, "auth.login_succeeded", "admin:"+adminIDStr, map[string]any{
		"via_recovery_code": isRecovery,
	})

	sessionToken, csrfToken, err = s.createSession(ctx, account.ID)
	if err != nil {
		return "", "", AdminIdentity{}, err
	}
	return sessionToken, csrfToken, admin, nil
}

func (s *Service) handleFailedLogin(ctx context.Context, adminID pgtype.UUID, failedAttempts int32, lastFailureAt pgtype.Timestamptz, reason string) {
	q := authstore.New(db.Conn(ctx, s.pool))
	adminIDStr := pgtypeconv.UUIDString(adminID)

	now := s.clock.Now()
	// Check failure window: if last failure was > 15m ago, reset attempts
	newAttempts := failedAttempts + 1
	if lastFailureAt.Valid && now.Sub(pgtypeconv.Time(lastFailureAt)) > FailureWindow {
		newAttempts = 1
	}

	var lockedUntil pgtype.Timestamptz
	var willLock bool
	if newAttempts >= MaxFailedAttempts {
		lockedUntil = pgtypeconv.Timestamptz(now.Add(LockoutDuration))
		willLock = true
	}

	_, _ = q.RecordLoginFailure(ctx, authstore.RecordLoginFailureParams{
		ID:          adminID,
		LockedUntil: lockedUntil,
	})

	// reason distinguishes which factor failed. It stays out of the HTTP
	// response (that is deliberately a generic 401) but belongs in the audit
	// trail, which only administrators read, so an operator can tell a
	// forgotten password from a drifted authenticator.
	s.recordAudit(ctx, nil, "system", "auth.login_failed", "admin:"+adminIDStr, map[string]any{
		"failed_attempts": newAttempts,
		"locked":          willLock,
		"reason":          reason,
	})

	if willLock {
		s.recordAudit(ctx, nil, "system", "auth.lockout", "admin:"+adminIDStr, map[string]any{
			"failed_attempts": newAttempts,
			"locked_until":    now.Add(LockoutDuration),
		})
	}
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

// CreateSessionForAdminID creates a fresh session for the given admin ID.
// Used when regenerating session identifiers on privilege changes.
func (s *Service) CreateSessionForAdminID(ctx context.Context, adminID string) (sessionToken, csrfToken string, err error) {
	uid, err := pgtypeconv.UUID(adminID)
	if err != nil {
		return "", "", fmt.Errorf("auth: invalid admin id: %w", err)
	}
	return s.createSession(ctx, uid)
}

// ValidatedSession is what Middleware needs from a valid session cookie.
type ValidatedSession struct {
	Admin     AdminIdentity
	CSRFToken string
}

// ValidateSession looks up plainToken, rejects it if expired or the owning
// account is disabled, and slides expiry forward.
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

	var lockedUntil *time.Time
	if row.LockedUntil.Valid {
		t := pgtypeconv.Time(row.LockedUntil)
		lockedUntil = &t
	}
	var lastLoginAt *time.Time
	if row.LastLoginAt.Valid {
		t := pgtypeconv.Time(row.LastLoginAt)
		lastLoginAt = &t
	}

	status := string(row.AdminStatus)
	if lockedUntil != nil && s.clock.Now().Before(*lockedUntil) {
		status = "locked"
	}

	return ValidatedSession{
		Admin: AdminIdentity{
			ID:                 pgtypeconv.UUIDString(row.AdminID),
			Email:              row.Email,
			FullName:           row.FullName,
			Role:               string(row.Role),
			Status:             status,
			MustChangePassword: row.MustChangePassword,
			MustReenrolTotp:    row.MustReenrolTotp,
			LockedUntil:        lockedUntil,
			LastLoginAt:        lastLoginAt,
			Locale:             row.Locale,
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

// ListAdmins lists all admin accounts in the system.
func (s *Service) ListAdmins(ctx context.Context) ([]AdminIdentity, error) {
	q := authstore.New(db.Conn(ctx, s.pool))
	rows, err := q.ListAdmins(ctx)
	if err != nil {
		return nil, fmt.Errorf("auth: list admins: %w", err)
	}
	now := s.clock.Now()
	res := make([]AdminIdentity, len(rows))
	for i, r := range rows {
		var lockedUntil *time.Time
		if r.LockedUntil.Valid {
			t := pgtypeconv.Time(r.LockedUntil)
			lockedUntil = &t
		}
		var lastLoginAt *time.Time
		if r.LastLoginAt.Valid {
			t := pgtypeconv.Time(r.LastLoginAt)
			lastLoginAt = &t
		}
		status := string(r.Status)
		if lockedUntil != nil && now.Before(*lockedUntil) {
			status = "locked"
		}
		res[i] = AdminIdentity{
			ID:                 pgtypeconv.UUIDString(r.ID),
			Email:              r.Email,
			FullName:           r.FullName,
			Role:               string(r.Role),
			Status:             status,
			MustChangePassword: r.MustChangePassword,
			MustReenrolTotp:    r.MustReenrolTotp,
			LockedUntil:        lockedUntil,
			LastLoginAt:        lastLoginAt,
			Locale:             r.Locale,
		}
	}
	return res, nil
}

// GetAdmin fetches a single admin account by ID.
func (s *Service) GetAdmin(ctx context.Context, id string) (AdminIdentity, error) {
	uid, err := pgtypeconv.UUID(id)
	if err != nil {
		return AdminIdentity{}, ErrAdminNotFound
	}
	q := authstore.New(db.Conn(ctx, s.pool))
	r, err := q.GetAdminAccountByID(ctx, uid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return AdminIdentity{}, ErrAdminNotFound
		}
		return AdminIdentity{}, fmt.Errorf("auth: get admin: %w", err)
	}

	var lockedUntil *time.Time
	if r.LockedUntil.Valid {
		t := pgtypeconv.Time(r.LockedUntil)
		lockedUntil = &t
	}
	var lastLoginAt *time.Time
	if r.LastLoginAt.Valid {
		t := pgtypeconv.Time(r.LastLoginAt)
		lastLoginAt = &t
	}
	status := string(r.Status)
	if lockedUntil != nil && s.clock.Now().Before(*lockedUntil) {
		status = "locked"
	}
	return AdminIdentity{
		ID:                 pgtypeconv.UUIDString(r.ID),
		Email:              r.Email,
		FullName:           r.FullName,
		Role:               string(r.Role),
		Status:             status,
		MustChangePassword: r.MustChangePassword,
		MustReenrolTotp:    r.MustReenrolTotp,
		LockedUntil:        lockedUntil,
		LastLoginAt:        lastLoginAt,
		Locale:             r.Locale,
	}, nil
}

// UpdateAdmin modifies an admin's fullName, role or status.
func (s *Service) UpdateAdmin(ctx context.Context, actorID, id string, fullName, role, status *string) (AdminIdentity, error) {
	uid, err := pgtypeconv.UUID(id)
	if err != nil {
		return AdminIdentity{}, ErrAdminNotFound
	}
	var fullText pgtype.Text
	if fullName != nil {
		fullText = pgtypeconv.Text(*fullName)
	}
	var roleNull authstore.NullAdminRole
	if role != nil {
		roleNull = authstore.NullAdminRole{AdminRole: authstore.AdminRole(*role), Valid: true}
	}
	var statusNull authstore.NullAdminStatus
	if status != nil {
		statusNull = authstore.NullAdminStatus{AdminStatus: authstore.AdminStatus(*status), Valid: true}
	}

	q := authstore.New(db.Conn(ctx, s.pool))
	r, err := q.UpdateAdmin(ctx, authstore.UpdateAdminParams{
		ID:       uid,
		FullName: fullText,
		Role:     roleNull,
		Status:   statusNull,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return AdminIdentity{}, ErrAdminNotFound
		}
		return AdminIdentity{}, fmt.Errorf("auth: update admin: %w", err)
	}

	// If status changed to disabled or role changed, revoke all sessions for this admin
	if (status != nil && *status == "disabled") || role != nil {
		_ = q.DeleteSessionsByAdminID(ctx, uid)
	}

	var lockedUntil *time.Time
	if r.LockedUntil.Valid {
		t := pgtypeconv.Time(r.LockedUntil)
		lockedUntil = &t
	}
	var lastLoginAt *time.Time
	if r.LastLoginAt.Valid {
		t := pgtypeconv.Time(r.LastLoginAt)
		lastLoginAt = &t
	}

	resStatus := string(r.Status)
	if lockedUntil != nil && s.clock.Now().Before(*lockedUntil) {
		resStatus = "locked"
	}

	s.recordAudit(ctx, nil, actorID, "admin.updated", "admin:"+id, map[string]any{
		"full_name": fullName,
		"role":      role,
		"status":    status,
	})

	return AdminIdentity{
		ID:                 pgtypeconv.UUIDString(r.ID),
		Email:              r.Email,
		FullName:           r.FullName,
		Role:               string(r.Role),
		Status:             resStatus,
		MustChangePassword: r.MustChangePassword,
		MustReenrolTotp:    r.MustReenrolTotp,
		LockedUntil:        lockedUntil,
		LastLoginAt:        lastLoginAt,
		Locale:             r.Locale,
	}, nil
}

// UpdateAdminLocale sets an admin's console language preference.
func (s *Service) UpdateAdminLocale(ctx context.Context, id string, locale string) (AdminIdentity, error) {
	uid, err := pgtypeconv.UUID(id)
	if err != nil {
		return AdminIdentity{}, ErrAdminNotFound
	}
	q := authstore.New(db.Conn(ctx, s.pool))
	r, err := q.UpdateAdminLocale(ctx, authstore.UpdateAdminLocaleParams{
		ID:     uid,
		Locale: locale,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return AdminIdentity{}, ErrAdminNotFound
		}
		return AdminIdentity{}, fmt.Errorf("auth: update admin locale: %w", err)
	}

	var lockedUntil *time.Time
	if r.LockedUntil.Valid {
		t := pgtypeconv.Time(r.LockedUntil)
		lockedUntil = &t
	}
	var lastLoginAt *time.Time
	if r.LastLoginAt.Valid {
		t := pgtypeconv.Time(r.LastLoginAt)
		lastLoginAt = &t
	}

	resStatus := string(r.Status)
	if lockedUntil != nil && s.clock.Now().Before(*lockedUntil) {
		resStatus = "locked"
	}

	s.recordAudit(ctx, nil, "admin:"+id, "admin.locale_updated", "admin:"+id, map[string]any{
		"locale": locale,
	})

	return AdminIdentity{
		ID:                 pgtypeconv.UUIDString(r.ID),
		Email:              r.Email,
		FullName:           r.FullName,
		Role:               string(r.Role),
		Status:             resStatus,
		MustChangePassword: r.MustChangePassword,
		MustReenrolTotp:    r.MustReenrolTotp,
		LockedUntil:        lockedUntil,
		LastLoginAt:        lastLoginAt,
		Locale:             r.Locale,
	}, nil
}

// ResetAdminPassword sets a new password for another admin and revokes their sessions.
func (s *Service) ResetAdminPassword(ctx context.Context, actorID, id, newPassword, reason string) error {
	if len(newPassword) < 12 {
		return ErrPasswordTooShort
	}
	uid, err := pgtypeconv.UUID(id)
	if err != nil {
		return ErrAdminNotFound
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}

	q := authstore.New(db.Conn(ctx, s.pool))
	_, err = q.ResetAdminPassword(ctx, authstore.ResetAdminPasswordParams{
		ID:                 uid,
		PasswordHash:       hash,
		MustChangePassword: true,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAdminNotFound
		}
		return fmt.Errorf("auth: reset password: %w", err)
	}

	_ = q.DeleteSessionsByAdminID(ctx, uid)

	s.recordAudit(ctx, nil, actorID, "admin.password_reset", "admin:"+id, map[string]any{
		"reason": reason,
	})
	return nil
}

// ForceAdminTotpReenrolment sets a new secret and forces another admin to re-enrol TOTP.
func (s *Service) ForceAdminTotpReenrolment(ctx context.Context, actorID, id, reason string) (totpSecret, otpauthURL string, err error) {
	uid, err := pgtypeconv.UUID(id)
	if err != nil {
		return "", "", ErrAdminNotFound
	}
	q := authstore.New(db.Conn(ctx, s.pool))
	account, err := q.GetAdminAccountByID(ctx, uid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", ErrAdminNotFound
		}
		return "", "", fmt.Errorf("auth: get admin: %w", err)
	}

	secret, url, err := GenerateTOTPSecret(account.Email)
	if err != nil {
		return "", "", err
	}
	secretEnc, err := encryptSecret(secret, s.totpEncKey)
	if err != nil {
		return "", "", err
	}

	_, err = q.SetAdminTotpSecret(ctx, authstore.SetAdminTotpSecretParams{
		ID:              uid,
		TotpSecretEnc:   secretEnc,
		MustReenrolTotp: true,
	})
	if err != nil {
		return "", "", fmt.Errorf("auth: set totp secret: %w", err)
	}

	_ = q.DeleteSessionsByAdminID(ctx, uid)

	s.recordAudit(ctx, nil, actorID, "admin.totp_reset", "admin:"+id, map[string]any{
		"reason": reason,
	})
	return secret, url, nil
}

// UnlockAdmin clears a lockout on an admin account.
func (s *Service) UnlockAdmin(ctx context.Context, actorID, id, reason string) error {
	uid, err := pgtypeconv.UUID(id)
	if err != nil {
		return ErrAdminNotFound
	}
	q := authstore.New(db.Conn(ctx, s.pool))
	_, err = q.UnlockAdminAccount(ctx, uid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAdminNotFound
		}
		return fmt.Errorf("auth: unlock admin: %w", err)
	}

	s.recordAudit(ctx, nil, actorID, "admin.unlocked", "admin:"+id, map[string]any{
		"reason": reason,
	})
	return nil
}

// UnlockAdminByEmail clears a lockout by email (for CLI recovery).
func (s *Service) UnlockAdminByEmail(ctx context.Context, email string) error {
	q := authstore.New(db.Conn(ctx, s.pool))
	r, err := q.UnlockAdminAccountByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAdminNotFound
		}
		return fmt.Errorf("auth: unlock admin by email: %w", err)
	}
	s.recordAudit(ctx, nil, "cli", "admin.unlocked", "admin:"+pgtypeconv.UUIDString(r.ID), map[string]any{
		"email": email,
		"via":   "cli",
	})
	return nil
}

// SetAdminPasswordByEmail replaces an admin's password from the operator
// console, revoking every existing session. It is the recovery path for a
// forgotten admin password: recovery codes only substitute for the TOTP
// factor, and ChangeOwnPassword needs a session the locked-out admin cannot
// obtain. Unlike ResetAdminPassword (one admin resetting another) it does not
// set must_change_password — the operator running the CLI is choosing the
// password themselves.
func (s *Service) SetAdminPasswordByEmail(ctx context.Context, email, newPassword string) error {
	if len(newPassword) < 12 {
		return ErrPasswordTooShort
	}
	q := authstore.New(db.Conn(ctx, s.pool))
	account, err := q.GetAdminAccountByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAdminNotFound
		}
		return fmt.Errorf("auth: get admin by email: %w", err)
	}

	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	if _, err := q.ResetAdminPassword(ctx, authstore.ResetAdminPasswordParams{
		ID:                 account.ID,
		PasswordHash:       hash,
		MustChangePassword: false,
	}); err != nil {
		return fmt.Errorf("auth: set password: %w", err)
	}

	_ = q.DeleteSessionsByAdminID(ctx, account.ID)

	s.recordAudit(ctx, nil, "cli", "admin.password_reset", "admin:"+pgtypeconv.UUIDString(account.ID), map[string]any{
		"email": email,
		"via":   "cli",
	})
	return nil
}

// ChangeOwnPassword allows the authenticated admin to change their own password.
func (s *Service) ChangeOwnPassword(ctx context.Context, adminID, currentPassword, newPassword, currentSessionToken string) error {
	if len(newPassword) < 12 {
		return ErrPasswordTooShort
	}
	uid, err := pgtypeconv.UUID(adminID)
	if err != nil {
		return ErrAdminNotFound
	}
	q := authstore.New(db.Conn(ctx, s.pool))
	account, err := q.GetAdminAccountByID(ctx, uid)
	if err != nil {
		return fmt.Errorf("auth: get account: %w", err)
	}

	ok, err := VerifyPassword(account.PasswordHash, currentPassword)
	if err != nil || !ok {
		return ErrInvalidPassword
	}

	newHash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}

	_, err = q.ResetAdminPassword(ctx, authstore.ResetAdminPasswordParams{
		ID:                 uid,
		PasswordHash:       newHash,
		MustChangePassword: false,
	})
	if err != nil {
		return fmt.Errorf("auth: update password: %w", err)
	}

	// Revoke other sessions
	if currentSessionToken != "" {
		_ = q.DeleteOtherSessionsByAdminID(ctx, authstore.DeleteOtherSessionsByAdminIDParams{
			AdminID:          uid,
			SessionTokenHash: hashToken(currentSessionToken, s.pepper),
		})
	}

	s.recordAudit(ctx, nil, "admin:"+adminID, "auth.password_changed", "admin:"+adminID, nil)
	return nil
}

// BeginTotpReenrolment initiates self-service TOTP re-enrolment.
func (s *Service) BeginTotpReenrolment(ctx context.Context, adminID string) (totpSecret, otpauthURL string, err error) {
	uid, err := pgtypeconv.UUID(adminID)
	if err != nil {
		return "", "", ErrAdminNotFound
	}
	q := authstore.New(db.Conn(ctx, s.pool))
	account, err := q.GetAdminAccountByID(ctx, uid)
	if err != nil {
		return "", "", fmt.Errorf("auth: get account: %w", err)
	}

	secret, url, err := GenerateTOTPSecret(account.Email)
	if err != nil {
		return "", "", err
	}
	secretEnc, err := encryptSecret(secret, s.totpEncKey)
	if err != nil {
		return "", "", err
	}

	_, err = q.SetAdminPendingTotp(ctx, authstore.SetAdminPendingTotpParams{
		ID:                   uid,
		TotpPendingSecretEnc: secretEnc,
	})
	if err != nil {
		return "", "", fmt.Errorf("auth: set pending totp: %w", err)
	}

	s.recordAudit(ctx, nil, "admin:"+adminID, "auth.totp_reenrol_started", "admin:"+adminID, nil)
	return secret, url, nil
}

// ConfirmTotpReenrolment finalises TOTP re-enrolment with a code from the pending secret.
func (s *Service) ConfirmTotpReenrolment(ctx context.Context, adminID, code string) error {
	uid, err := pgtypeconv.UUID(adminID)
	if err != nil {
		return ErrAdminNotFound
	}
	q := authstore.New(db.Conn(ctx, s.pool))
	account, err := q.GetAdminAccountByID(ctx, uid)
	if err != nil {
		return fmt.Errorf("auth: get account: %w", err)
	}
	if len(account.TotpPendingSecretEnc) == 0 {
		return ErrNoPendingTotp
	}

	secret, err := decryptSecret(account.TotpPendingSecretEnc, s.totpEncKey)
	if err != nil {
		return fmt.Errorf("auth: decrypt pending secret: %w", err)
	}
	if !ValidateTOTPCode(secret, code) {
		return ErrInvalidCredentials
	}

	_, err = q.ConfirmAdminPendingTotp(ctx, uid)
	if err != nil {
		return fmt.Errorf("auth: confirm pending totp: %w", err)
	}

	s.recordAudit(ctx, nil, "admin:"+adminID, "auth.totp_reenrol_confirmed", "admin:"+adminID, nil)
	return nil
}

// RegenerateRecoveryCodes creates new single-use recovery codes for the admin, replacing unused ones.
func (s *Service) RegenerateRecoveryCodes(ctx context.Context, adminID string) ([]string, error) {
	uid, err := pgtypeconv.UUID(adminID)
	if err != nil {
		return nil, ErrAdminNotFound
	}

	codes, err := GenerateRecoveryCodes(RecoveryCodeCount)
	if err != nil {
		return nil, err
	}

	q := authstore.New(db.Conn(ctx, s.pool))
	// Delete unused codes first
	_ = q.DeleteUnusedRecoveryCodesByAdminID(ctx, uid)

	// Insert hashed codes
	for _, c := range codes {
		hash, err := HashPassword(c)
		if err != nil {
			return nil, err
		}
		if err := q.InsertRecoveryCode(ctx, authstore.InsertRecoveryCodeParams{
			ID:       pgtypeconv.NewUUID(),
			AdminID:  uid,
			CodeHash: hash,
		}); err != nil {
			return nil, fmt.Errorf("auth: insert recovery code: %w", err)
		}
	}

	s.recordAudit(ctx, nil, "admin:"+adminID, "auth.recovery_codes_regenerated", "admin:"+adminID, map[string]any{
		"count": len(codes),
	})
	return codes, nil
}

// ValidateKioskToken looks up a kiosk bearer token, rejects a disabled
// kiosk, and records the touch.
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

// hashToken computes HMAC-SHA256(token, pepper).
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
