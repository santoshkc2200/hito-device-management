# Phase 7 — Staff Identity, Self-Signup and the Staff PWA — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give hospital staff their own login — Microsoft/Entra single sign-on or an employee number and password — a phone PWA that shows their QR code, their loans and the device catalogue, and give administrators the ability to view and print any user's QR at any time.

**Architecture:** A third principal is added beside administrators and kiosks. `internal/platform/staffauth` owns staff accounts, external identity links, staff sessions and the Entra OIDC client; it never looks a user up, taking and returning user IDs only. The linking-and-provisioning ladder is orchestrated in `apiserver`, exactly as `RegisterWithCard` already orchestrates identity plus credentials inside one transaction. The staff app is a new Vite PWA at `hdms-frontend/apps/staff`, reusing the existing workspace packages.

**Tech Stack:** Go 1.26.1, pgx/v5 + sqlc, goose migrations, oapi-codegen (spec-first), `github.com/coreos/go-oidc/v3` (new dependency), React 19 + TanStack Router/Query, Vite 8, vite-plugin-pwa, vitest + Testing Library, Playwright.

**Spec:** `docs/superpowers/specs/2026-09-15-staff-identity-design.md`

## Global Constraints

- **API paths are `/v1/...`, not `/api/...`.** The spec's prose used `/api/staff/*`; the server mounts everything under `/v1` (`servers: - url: /v1` in `api/openapi.yaml`). Every staff path in this plan is `/v1/staff/...`.
- **The contract is spec-first and additive only.** Edit `hdms-backend/api/openapi.yaml`, then run `task generate`. Never hand-edit `internal/platform/httpx/gen/api.gen.go` or `packages/api-client/src`. Bump `info.version` to `1.2.0` once, in Task 6.
- **Every path must be classified in `auth.RequiredRoles`** (`internal/platform/auth/roles.go`) or the admin middleware answers 403 "Unclassified operation". Staff paths are handled by an earlier branch and must *not* appear there.
- **Module boundaries are linted.** `platform/*` may not import a module's `…api` package. Cross-module orchestration happens in `apiserver`.
- **Both locales ship together.** Every user-visible string goes through `@hdms/i18n`; `en.ts` and `ja.ts` change in the same commit. No string literals in JSX.
- **Migrations are goose, numbered, and never edited once committed.** Next free number is `0018`.
- **Test naming** follows the existing convention: descriptive camelCase Go test names inside `Test…` functions, e.g. `TestAStaffCookieIsRejectedOnAdminRoutes`.
- **Commit style** is Conventional Commits with a phase scope, e.g. `feat(phase-7a): …`.
- **Verification commands:** `task lint`, `task test`, `task test:integration`, `task build`. Integration tests need Docker (testcontainers).

---

### Task 1: Allow `self:microsoft` as a registration provenance

The identity module currently refuses any `registered_by` that is not `admin:…`, `import` or `import:…`. Self-signup needs a fourth, and the kiosk prefix must stay refused — that is INV-11 and it is not being relaxed.

**Files:**
- Modify: `hdms-backend/internal/modules/identity/internal/domain/user.go:59-68`
- Test: `hdms-backend/internal/modules/identity/internal/domain/user_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `domain.ValidateRegisteredBy` accepts the exact string `self:microsoft`; `identityapi.CreateUserParams{RegisteredBy: "self:microsoft"}` now succeeds.

- [ ] **Step 1: Write the failing test**

Append to `hdms-backend/internal/modules/identity/internal/domain/user_test.go`:

```go
func TestValidateRegisteredByAcceptsSelfMicrosoftAndStillRefusesKiosk(t *testing.T) {
	got, err := ValidateRegisteredBy("self:microsoft")
	if err != nil {
		t.Fatalf("ValidateRegisteredBy(self:microsoft) = %v, want nil", err)
	}
	if got != "self:microsoft" {
		t.Fatalf("canonical form = %q, want %q", got, "self:microsoft")
	}

	if _, err := ValidateRegisteredBy("kiosk:abc"); err == nil {
		t.Fatal("ValidateRegisteredBy(kiosk:abc) = nil, want an error — INV-11 still holds")
	}
	if _, err := ValidateRegisteredBy("self:something-else"); err == nil {
		t.Fatal("only self:microsoft is a recognised self-registration provenance")
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

```bash
cd hdms-backend && go test ./internal/modules/identity/internal/domain/ -run TestValidateRegisteredBy -v
```

Expected: FAIL — `ValidateRegisteredBy(self:microsoft) = identity: registered_by is invalid, want nil`.

- [ ] **Step 3: Make it pass**

In `user.go`, replace the body of `ValidateRegisteredBy` with:

```go
func ValidateRegisteredBy(raw string) (string, error) {
	v := strings.TrimSpace(raw)
	if v == "" || strings.HasPrefix(v, "kiosk:") {
		return "", ErrRegisteredByInvalid
	}
	// 'self:microsoft' is the Phase 7 self-signup provenance (ADR-0012): the
	// authorising decision moved to the hospital's Entra tenant. The kiosk
	// prefix stays refused — a walk-up terminal still never creates a user.
	if v == "self:microsoft" {
		return v, nil
	}
	if v != "import" && !strings.HasPrefix(v, "admin:") && !strings.HasPrefix(v, "import:") {
		return "", ErrRegisteredByInvalid
	}
	return v, nil
}
```

- [ ] **Step 4: Run the package tests**

```bash
cd hdms-backend && go test ./internal/modules/identity/... -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add hdms-backend/internal/modules/identity/internal/domain/user.go hdms-backend/internal/modules/identity/internal/domain/user_test.go
git commit -m "feat(phase-7a): accept self:microsoft as a registration provenance"
```

---

### Task 2: Migration 0018 and the generated staff-auth store

**Files:**
- Create: `hdms-backend/migrations/0018_staff_auth.sql`
- Create: `hdms-backend/queries/staffauth/staffauth.sql`
- Modify: `hdms-backend/sqlc.yaml` (append one entry)
- Test: `hdms-backend/test/integration/staffauth_schema_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: tables `staff_accounts`, `staff_identities`, `staff_sessions`, `oauth_login_states`; index `users_email_live_uk`; generated package `staffauthstore` at `internal/platform/staffauth/store` with `New(db)` and the queries named in Step 3.

- [ ] **Step 1: Write the migration**

Create `hdms-backend/migrations/0018_staff_auth.sql`:

```sql
-- +goose Up
-- +goose StatementBegin

-- Phase 7: the staff authentication realm (ADR-0009). Separate from
-- admin_accounts on purpose: a staff session must never be mistaken for an
-- administrator session, and no existing admin query should have to start
-- excluding rows.

CREATE TABLE staff_accounts (
    id                    uuid PRIMARY KEY,
    -- No FK: user_id crosses the identity module boundary, exactly as
    -- credentials.subject_id does (INV-12).
    user_id               uuid NOT NULL UNIQUE,
    password_hash         text,                   -- NULL for Microsoft-only accounts
    must_change_password  boolean NOT NULL DEFAULT false,
    profile_complete      boolean NOT NULL DEFAULT true,
    failed_attempts       integer NOT NULL DEFAULT 0,
    last_failure_at       timestamptz,
    locked_until          timestamptz,
    last_login_at         timestamptz,
    created_at            timestamptz NOT NULL DEFAULT now(),
    created_by            text NOT NULL,          -- 'admin:<id>' | 'self:microsoft'
    updated_at            timestamptz NOT NULL DEFAULT now()
);

-- There is deliberately no status column here: login reads users.status
-- live, so suspend and archive take effect on the next request and there is
-- no second copy of the truth to reconcile.

CREATE TABLE staff_identities (
    id                uuid PRIMARY KEY,
    staff_account_id  uuid NOT NULL REFERENCES staff_accounts(id) ON DELETE CASCADE,
    provider          text NOT NULL,              -- 'microsoft'
    subject           text NOT NULL,              -- the Entra object id (oid)
    tenant_id         text NOT NULL,
    email_at_link     text,
    linked_at         timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT staff_identities_provider_subject_uk UNIQUE (provider, subject)
);

CREATE INDEX staff_identities_account_idx ON staff_identities (staff_account_id);

CREATE TABLE staff_sessions (
    id                  uuid PRIMARY KEY,
    staff_account_id    uuid NOT NULL REFERENCES staff_accounts(id) ON DELETE CASCADE,
    session_token_hash  bytea NOT NULL,
    csrf_token          text NOT NULL,
    created_at          timestamptz NOT NULL DEFAULT now(),
    last_seen_at        timestamptz NOT NULL DEFAULT now(),
    expires_at          timestamptz NOT NULL
);

CREATE UNIQUE INDEX staff_sessions_token_hash_uk ON staff_sessions (session_token_hash);
CREATE INDEX staff_sessions_account_idx ON staff_sessions (staff_account_id);

-- The PKCE verifier lives here rather than in a cookie, so nothing secret
-- crosses the redirect. Rows are single-use and swept after expiry.
CREATE TABLE oauth_login_states (
    state_hash    bytea PRIMARY KEY,
    verifier_enc  bytea NOT NULL,
    redirect_to   text,
    created_at    timestamptz NOT NULL DEFAULT now(),
    expires_at    timestamptz NOT NULL,
    consumed_at   timestamptz
);

CREATE INDEX oauth_login_states_expiry_idx ON oauth_login_states (expires_at);

-- Email becomes a linking key on Microsoft sign-in, so it must be unique
-- among live users. Archived duplicates stay allowed, as with employee_no.
CREATE UNIQUE INDEX users_email_live_uk
    ON users (lower(email)) WHERE email IS NOT NULL AND status <> 'archived';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS users_email_live_uk;
DROP TABLE oauth_login_states;
DROP TABLE staff_sessions;
DROP TABLE staff_identities;
DROP TABLE staff_accounts;

-- +goose StatementEnd
```

- [ ] **Step 2: Write the queries**

Create `hdms-backend/queries/staffauth/staffauth.sql`:

```sql
-- name: CreateStaffAccount :one
INSERT INTO staff_accounts (id, user_id, password_hash, must_change_password, profile_complete, created_by)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetStaffAccountByID :one
SELECT * FROM staff_accounts WHERE id = $1;

-- name: GetStaffAccountByUserID :one
SELECT * FROM staff_accounts WHERE user_id = $1;

-- name: SetStaffPassword :one
UPDATE staff_accounts
SET password_hash = $2, must_change_password = $3, failed_attempts = 0,
    locked_until = NULL, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: MarkStaffProfileComplete :one
UPDATE staff_accounts SET profile_complete = true, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: RecordStaffLoginSuccess :exec
UPDATE staff_accounts
SET failed_attempts = 0, locked_until = NULL, last_login_at = now(), updated_at = now()
WHERE id = $1;

-- name: RecordStaffLoginFailure :one
UPDATE staff_accounts
SET failed_attempts = failed_attempts + 1,
    last_failure_at = now(),
    locked_until = CASE WHEN failed_attempts + 1 >= $2 THEN now() + $3::interval ELSE locked_until END,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: LinkStaffIdentity :one
INSERT INTO staff_identities (id, staff_account_id, provider, subject, tenant_id, email_at_link)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetStaffIdentity :one
SELECT * FROM staff_identities WHERE provider = $1 AND subject = $2;

-- name: CreateStaffSession :one
INSERT INTO staff_sessions (id, staff_account_id, session_token_hash, csrf_token, expires_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetStaffSessionByTokenHash :one
SELECT * FROM staff_sessions WHERE session_token_hash = $1;

-- name: SlideStaffSession :exec
UPDATE staff_sessions SET last_seen_at = now(), expires_at = $2 WHERE id = $1;

-- name: DeleteStaffSessionByTokenHash :exec
DELETE FROM staff_sessions WHERE session_token_hash = $1;

-- name: DeleteStaffSessionsForAccount :exec
DELETE FROM staff_sessions WHERE staff_account_id = $1;

-- name: DeleteExpiredStaffSessions :exec
DELETE FROM staff_sessions WHERE expires_at < now();

-- name: CreateLoginState :exec
INSERT INTO oauth_login_states (state_hash, verifier_enc, redirect_to, expires_at)
VALUES ($1, $2, $3, $4);

-- name: ConsumeLoginState :one
UPDATE oauth_login_states SET consumed_at = now()
WHERE state_hash = $1 AND consumed_at IS NULL AND expires_at > now()
RETURNING *;

-- name: DeleteExpiredLoginStates :exec
DELETE FROM oauth_login_states WHERE expires_at < now();
```

- [ ] **Step 3: Add the sqlc target and generate**

Append to `hdms-backend/sqlc.yaml`:

```yaml
  - engine: "postgresql"
    queries: "queries/staffauth"
    schema: "migrations"
    gen:
      go:
        package: "staffauthstore"
        out: "internal/platform/staffauth/store"
        sql_package: "pgx/v5"
        emit_json_tags: true
        emit_interface: true
```

Run:

```bash
cd hdms-backend && sqlc generate && go build ./...
```

Expected: `internal/platform/staffauth/store/` now contains `staffauth.sql.go`, `models.go`, `querier.go`, `db.go`.

- [ ] **Step 4: Write the schema integration test**

Create `hdms-backend/test/integration/staffauth_schema_test.go`:

```go
//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/hito-hospital/hdms/test/testdb"
)

func TestStaffIdentityIsUniquePerProviderSubject(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	var userID, accountID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO users (id, employee_no, full_name, registered_by)
		VALUES (gen_random_uuid(), 'E-STAFF-1', 'Test Staff', 'admin:test') RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO staff_accounts (id, user_id, created_by)
		VALUES (gen_random_uuid(), $1, 'admin:test') RETURNING id`, userID).Scan(&accountID); err != nil {
		t.Fatalf("insert staff account: %v", err)
	}

	insert := `INSERT INTO staff_identities (id, staff_account_id, provider, subject, tenant_id)
	           VALUES (gen_random_uuid(), $1, 'microsoft', 'oid-1', 'tenant-1')`
	if _, err := pool.Exec(ctx, insert, accountID); err != nil {
		t.Fatalf("first link: %v", err)
	}
	if _, err := pool.Exec(ctx, insert, accountID); err == nil {
		t.Fatal("second link with the same (provider, subject) succeeded; the unique constraint is missing")
	}
}

func TestLiveUserEmailsAreUniqueButArchivedOnesAreNot(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	mk := `INSERT INTO users (id, employee_no, full_name, email, status, registered_by)
	       VALUES (gen_random_uuid(), $1, 'Test Staff', $2, $3, 'admin:test')`

	if _, err := pool.Exec(ctx, mk, "E-MAIL-1", "person@hospital.example", "active"); err != nil {
		t.Fatalf("first live user: %v", err)
	}
	if _, err := pool.Exec(ctx, mk, "E-MAIL-2", "PERSON@hospital.example", "active"); err == nil {
		t.Fatal("a second live user with the same email (different case) was allowed")
	}
	if _, err := pool.Exec(ctx, mk, "E-MAIL-3", "person@hospital.example", "archived"); err != nil {
		t.Fatalf("an archived duplicate must still be allowed: %v", err)
	}
}
```

- [ ] **Step 5: Run migrations and the test**

```bash
cd hdms-backend && go test -race -tags=integration ./test/integration/ -run 'TestStaffIdentityIsUnique|TestLiveUserEmails' -v
```

Expected: PASS. If `users_email_live_uk` fails to create because fixture data already contains duplicate emails, clear the local database (`task down && task dev`) — this deployment carries no live users.

- [ ] **Step 6: Commit**

```bash
git add hdms-backend/migrations/0018_staff_auth.sql hdms-backend/queries/staffauth hdms-backend/sqlc.yaml hdms-backend/internal/platform/staffauth/store hdms-backend/test/integration/staffauth_schema_test.go
git commit -m "feat(phase-7b): add the staff authentication schema and its generated store"
```

---

### Task 3: The staffauth service — accounts, password login, sessions

**Files:**
- Create: `hdms-backend/internal/platform/staffauth/service.go`
- Create: `hdms-backend/internal/platform/staffauth/session.go`
- Create: `hdms-backend/internal/platform/staffauth/cookies.go`
- Test: `hdms-backend/test/integration/staffauth_test.go`

**Interfaces:**
- Consumes: `staffauthstore` (Task 2); `auth.HashPassword` / `auth.VerifyPassword` from `internal/platform/auth/password.go`; `db.Pool`, `db.Conn`, `db.NewTxManager`, `pgtypeconv`.
- Produces:

```go
type Account struct {
	ID, UserID         string
	HasPassword        bool
	MustChangePassword bool
	ProfileComplete    bool
}
type ValidatedSession struct {
	Account   Account
	CSRFToken string
}
func New(pool *db.Pool, sessionTTL time.Duration) *Service
func (s *Service) EnsureAccount(ctx context.Context, userID, createdBy string, profileComplete bool) (Account, error)
func (s *Service) AccountForUser(ctx context.Context, userID string) (Account, error)
func (s *Service) SetPassword(ctx context.Context, accountID, plaintext string, mustChange bool) error
func (s *Service) VerifyPassword(ctx context.Context, accountID, plaintext string) error
func (s *Service) ChangeOwnPassword(ctx context.Context, accountID, current, next string) error
func (s *Service) MarkProfileComplete(ctx context.Context, accountID string) (Account, error)
func (s *Service) StartSession(ctx context.Context, accountID string) (sessionToken, csrfToken string, err error)
func (s *Service) ValidateSession(ctx context.Context, sessionToken string) (ValidatedSession, error)
func (s *Service) RevokeSession(ctx context.Context, sessionToken string) error
func (s *Service) SessionTTL() time.Duration
var ErrAccountLocked, ErrInvalidCredentials, ErrNoPasswordSet, ErrSessionInvalid error
func SessionCookie(token string, ttlSeconds int) *http.Cookie
func CSRFCookie(token string, ttlSeconds int) *http.Cookie
func ExpiredSessionCookie() *http.Cookie
func ExpiredCSRFCookie() *http.Cookie
```

- [ ] **Step 1: Write the failing integration test**

Create `hdms-backend/test/integration/staffauth_test.go`:

```go
//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/staffauth"
	"github.com/hito-hospital/hdms/test/testdb"
)

func newStaffAuth(t *testing.T) (*staffauth.Service, string) {
	t.Helper()
	pool := testdb.New(t)
	var userID string
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO users (id, employee_no, full_name, registered_by)
		VALUES (gen_random_uuid(), 'E-SA-1', 'Test Staff', 'admin:test') RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return staffauth.New(pool, time.Hour), userID
}

func TestPasswordLoginIssuesAStaffSession(t *testing.T) {
	svc, userID := newStaffAuth(t)
	ctx := context.Background()

	account, err := svc.EnsureAccount(ctx, userID, "admin:test", true)
	if err != nil {
		t.Fatalf("EnsureAccount: %v", err)
	}
	if err := svc.SetPassword(ctx, account.ID, "correct-horse-battery", true); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	if err := svc.VerifyPassword(ctx, account.ID, "correct-horse-battery"); err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}

	sessionToken, csrfToken, err := svc.StartSession(ctx, account.ID)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	validated, err := svc.ValidateSession(ctx, sessionToken)
	if err != nil {
		t.Fatalf("ValidateSession: %v", err)
	}
	if validated.Account.ID != account.ID || validated.CSRFToken != csrfToken {
		t.Fatalf("validated session = %+v, want account %s and csrf %s", validated, account.ID, csrfToken)
	}
	if !validated.Account.MustChangePassword {
		t.Fatal("a password set with mustChange=true must surface MustChangePassword")
	}

	if err := svc.RevokeSession(ctx, sessionToken); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}
	if _, err := svc.ValidateSession(ctx, sessionToken); !errors.Is(err, staffauth.ErrSessionInvalid) {
		t.Fatalf("ValidateSession after revoke = %v, want ErrSessionInvalid", err)
	}
}

func TestRepeatedBadPasswordsLockTheStaffAccount(t *testing.T) {
	svc, userID := newStaffAuth(t)
	ctx := context.Background()

	account, err := svc.EnsureAccount(ctx, userID, "admin:test", true)
	if err != nil {
		t.Fatalf("EnsureAccount: %v", err)
	}
	if err := svc.SetPassword(ctx, account.ID, "correct-horse-battery", false); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}

	for i := 0; i < staffauth.MaxFailedAttempts; i++ {
		if err := svc.VerifyPassword(ctx, account.ID, "wrong"); !errors.Is(err, staffauth.ErrInvalidCredentials) {
			t.Fatalf("attempt %d = %v, want ErrInvalidCredentials", i+1, err)
		}
	}
	if err := svc.VerifyPassword(ctx, account.ID, "correct-horse-battery"); !errors.Is(err, staffauth.ErrAccountLocked) {
		t.Fatalf("after %d failures the correct password = %v, want ErrAccountLocked", staffauth.MaxFailedAttempts, err)
	}
}

func TestAMicrosoftOnlyAccountHasNoPasswordToVerify(t *testing.T) {
	svc, userID := newStaffAuth(t)
	ctx := context.Background()

	account, err := svc.EnsureAccount(ctx, userID, "self:microsoft", false)
	if err != nil {
		t.Fatalf("EnsureAccount: %v", err)
	}
	if account.HasPassword {
		t.Fatal("a freshly provisioned Microsoft account must have no password")
	}
	if err := svc.VerifyPassword(ctx, account.ID, "anything"); !errors.Is(err, staffauth.ErrNoPasswordSet) {
		t.Fatalf("VerifyPassword on a passwordless account = %v, want ErrNoPasswordSet", err)
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

```bash
cd hdms-backend && go test -race -tags=integration ./test/integration/ -run TestPasswordLoginIssuesAStaffSession -v
```

Expected: FAIL to compile — `no required module provides package .../internal/platform/staffauth`.

- [ ] **Step 3: Write the service**

Create `hdms-backend/internal/platform/staffauth/service.go`:

```go
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
	if !auth.VerifyPassword(plaintext, row.PasswordHash.String) {
		if _, failErr := q.RecordStaffLoginFailure(ctx, staffauthstore.RecordStaffLoginFailureParams{
			ID:      row.ID,
			Column2: int32(MaxFailedAttempts),
			Column3: pgtypeconv.Interval(LockoutWindow),
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
```

If `pgtypeconv` has no `Interval` helper, add one next to the existing converters:

```go
// Interval converts a Go duration to the pgtype the interval parameter of a
// generated query expects.
func Interval(d time.Duration) pgtype.Interval {
	return pgtype.Interval{Microseconds: d.Microseconds(), Valid: true}
}
```

Check the generated parameter names in `internal/platform/staffauth/store/staffauth.sql.go` for `RecordStaffLoginFailure` before writing the call — sqlc names positional parameters `Column2`/`Column3` only when the query gives it nothing better. If the generated names differ, use the generated ones.

- [ ] **Step 4: Write the session half**

Create `hdms-backend/internal/platform/staffauth/session.go`:

```go
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
```

- [ ] **Step 5: Write the cookies**

Create `hdms-backend/internal/platform/staffauth/cookies.go`:

```go
package staffauth

import "net/http"

const (
	SessionCookieName = "hdms_staff_session"
	CSRFCookieName    = "hdms_staff_csrf"
	CSRFHeaderName    = "X-CSRF-Token"
)

// SessionCookie mirrors auth.SessionCookie but under a distinct name, so a
// staff session and an administrator session can coexist in one browser and
// can never be confused for one another.
func SessionCookie(token string, ttlSeconds int) *http.Cookie {
	return &http.Cookie{
		Name: SessionCookieName, Value: token, Path: "/",
		HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode, MaxAge: ttlSeconds,
	}
}

// CSRFCookie is readable by the app, which echoes it back in X-CSRF-Token.
func CSRFCookie(token string, ttlSeconds int) *http.Cookie {
	return &http.Cookie{
		Name: CSRFCookieName, Value: token, Path: "/",
		HttpOnly: false, Secure: true, SameSite: http.SameSiteLaxMode, MaxAge: ttlSeconds,
	}
}

func ExpiredSessionCookie() *http.Cookie {
	return &http.Cookie{Name: SessionCookieName, Value: "", Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode, MaxAge: -1}
}

func ExpiredCSRFCookie() *http.Cookie {
	return &http.Cookie{Name: CSRFCookieName, Value: "", Path: "/", HttpOnly: false, Secure: true, SameSite: http.SameSiteLaxMode, MaxAge: -1}
}
```

- [ ] **Step 6: Run the tests**

```bash
cd hdms-backend && go test -race -tags=integration ./test/integration/ -run 'TestPasswordLogin|TestRepeatedBadPasswords|TestAMicrosoftOnlyAccount' -v && go vet ./internal/platform/staffauth/...
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add hdms-backend/internal/platform/staffauth hdms-backend/internal/platform/pgtypeconv hdms-backend/test/integration/staffauth_test.go
git commit -m "feat(phase-7b): staff accounts, password verification and staff sessions"
```

---

### Task 4: The staff middleware branch and scope separation

The existing middleware has two branches: a kiosk bearer token, then an administrator cookie. A third branch goes first for `/v1/staff/` paths, and the administrator branch must refuse them. Both refusals are tested.

**Files:**
- Create: `hdms-backend/internal/platform/staffauth/middleware.go`
- Modify: `hdms-backend/internal/platform/auth/middleware.go:18-33` (path guard)
- Test: `hdms-backend/test/integration/staff_scope_test.go`

**Interfaces:**
- Consumes: `Service.ValidateSession`, the cookie names from Task 3.
- Produces:

```go
func (s *Service) Middleware(next http.Handler) http.Handler   // staffauth
func AccountFromContext(ctx context.Context) (Account, bool)
func IsStaffPath(path string) bool                              // exported for auth's guard
```

- [ ] **Step 1: Write the failing test**

Create `hdms-backend/test/integration/staff_scope_test.go`:

```go
//go:build integration

package integration

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// The two refusals that keep the realms apart. If either regresses, a staff
// phone can reach the admin console or vice versa.
func TestAnAdminCookieIsRejectedOnStaffRoutes(t *testing.T) {
	env := newHTTPTestEnv(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/staff/me", nil)
	req.AddCookie(&http.Cookie{Name: "hdms_session", Value: env.AdminSessionToken})

	rec := httptest.NewRecorder()
	env.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET /v1/staff/me with an admin cookie = %d, want 401", rec.Code)
	}
}

func TestAStaffCookieIsRejectedOnAdminRoutes(t *testing.T) {
	env := newHTTPTestEnv(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/users", nil)
	req.AddCookie(&http.Cookie{Name: "hdms_staff_session", Value: env.StaffSessionToken})

	rec := httptest.NewRecorder()
	env.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET /v1/users with a staff cookie = %d, want 401", rec.Code)
	}
}
```

`newHTTPTestEnv` is the existing helper pattern in `test/integration/httpserver_test.go`. Extend that helper to also create a staff account with a session and expose `StaffSessionToken`; read the file first and follow whatever shape it already uses rather than inventing a second harness.

- [ ] **Step 2: Run it and watch it fail**

```bash
cd hdms-backend && go test -race -tags=integration ./test/integration/ -run 'TestAnAdminCookieIsRejected|TestAStaffCookieIsRejected' -v
```

Expected: FAIL — the staff path is unclassified, so the admin middleware answers 403 rather than 401, and `StaffSessionToken` does not exist yet.

- [ ] **Step 3: Write the staff middleware**

Create `hdms-backend/internal/platform/staffauth/middleware.go`:

```go
package staffauth

import (
	"context"
	"net/http"
	"strings"

	"github.com/hito-hospital/hdms/internal/platform/httpx"
)

// staffPathPrefix is the whole of the staff realm's surface. Everything under
// it is served by this middleware; everything outside it is invisible to a
// staff session.
const staffPathPrefix = "/v1/staff/"

// staffPublicPaths need no session: they are how a session is obtained.
var staffPublicPaths = map[string]struct{}{
	"/v1/staff/auth/password":           {},
	"/v1/staff/auth/microsoft/start":    {},
	"/v1/staff/auth/microsoft/callback": {},
}

// IsStaffPath reports whether a request path belongs to the staff realm. The
// administrator middleware calls this to hand the request over rather than
// classifying it, which is what keeps the two role tables from merging.
func IsStaffPath(path string) bool {
	clean, _, _ := strings.Cut(path, "?")
	return strings.HasPrefix(clean, staffPathPrefix)
}

type ctxKey int

const accountKey ctxKey = iota

func contextWithAccount(ctx context.Context, a Account) context.Context {
	return context.WithValue(ctx, accountKey, a)
}

// AccountFromContext returns the staff account a handler is acting for.
func AccountFromContext(ctx context.Context) (Account, bool) {
	a, ok := ctx.Value(accountKey).(Account)
	return a, ok
}

// Middleware authenticates the staff realm. It runs before the administrator
// middleware and returns without calling it for staff paths, so an
// administrator cookie is never even looked at here.
func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !IsStaffPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		clean, _, _ := strings.Cut(r.URL.Path, "?")
		if _, ok := staffPublicPaths[clean]; ok {
			next.ServeHTTP(w, r)
			return
		}

		cookie, err := r.Cookie(SessionCookieName)
		if err != nil {
			writeStaffUnauthorized(w, r, "Authentication required")
			return
		}
		validated, err := s.ValidateSession(r.Context(), cookie.Value)
		if err != nil {
			writeStaffUnauthorized(w, r, "Session invalid or expired")
			return
		}

		if isMutating(r.Method) {
			if r.Header.Get(CSRFHeaderName) != validated.CSRFToken || validated.CSRFToken == "" {
				p := httpx.NewProblem("forbidden", "Forbidden", http.StatusForbidden)
				p.Detail = "CSRF token missing or mismatched"
				httpx.WriteProblem(w, r, p)
				return
			}
		}

		// A temporary password and an incomplete profile are both dead ends
		// until they are resolved; the app has exactly one screen for each.
		if validated.Account.MustChangePassword && !isPasswordChangeAllowed(r.Method, clean) {
			p := httpx.NewProblem("password-change-required", "Password change required", http.StatusForbidden)
			p.Detail = "You must change your password before doing anything else."
			httpx.WriteProblem(w, r, p)
			return
		}
		if !validated.Account.ProfileComplete && !isProfileCompletionAllowed(r.Method, clean) {
			p := httpx.NewProblem("profile-incomplete", "Profile incomplete", http.StatusForbidden)
			p.Detail = "Your employee number is needed before you can use the system."
			httpx.WriteProblem(w, r, p)
			return
		}

		ctx := contextWithAccount(r.Context(), validated.Account)
		ctx = httpx.ContextWithActor(ctx, "staff:"+validated.Account.UserID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func isMutating(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func isPasswordChangeAllowed(method, path string) bool {
	return (method == http.MethodPost && path == "/v1/staff/me/password") ||
		(method == http.MethodPost && path == "/v1/staff/auth/logout") ||
		(method == http.MethodGet && path == "/v1/staff/me")
}

func isProfileCompletionAllowed(method, path string) bool {
	return (method == http.MethodPost && path == "/v1/staff/me/profile") ||
		(method == http.MethodPost && path == "/v1/staff/auth/logout") ||
		(method == http.MethodGet && path == "/v1/staff/me")
}

func writeStaffUnauthorized(w http.ResponseWriter, r *http.Request, detail string) {
	p := httpx.NewProblem("unauthorized", "Unauthorized", http.StatusUnauthorized)
	p.Detail = detail
	httpx.WriteProblem(w, r, p)
}
```

- [ ] **Step 4: Teach the administrator middleware to stay out**

In `hdms-backend/internal/platform/auth/middleware.go`, immediately after the `unauthenticatedPaths` check inside `Middleware`, add:

```go
		// The staff realm is served by staffauth.Middleware, which runs
		// first. If a request reaches here on a staff path, the staff
		// middleware already declined it — answering 401 rather than
		// falling through to the administrator branches is what stops an
		// administrator cookie from ever authenticating a staff request.
		if staffauth.IsStaffPath(r.URL.Path) {
			writeUnauthorized(w, r, "Authentication required")
			return
		}
```

Import `"github.com/hito-hospital/hdms/internal/platform/staffauth"`. If that import creates a cycle (it will if `staffauth` ever imports `auth` for `HashPassword`), break it by moving `IsStaffPath` into a tiny leaf package `internal/platform/httpx/realm` that both import, and call `realm.IsStaffPath`. Check with `go build ./...` before choosing.

- [ ] **Step 5: Chain the middleware in the composition root**

In `hdms-backend/cmd/hdms-api/main.go`, wrap the handler so staff runs outermost:

```go
	handler = staffAuthSvc.Middleware(authSvc.Middleware(handler))
```

Read the file first; match however the existing chain is written.

- [ ] **Step 6: Run the tests**

```bash
cd hdms-backend && go test -race -tags=integration ./test/integration/ -run 'TestAnAdminCookieIsRejected|TestAStaffCookieIsRejected' -v && go test ./... && golangci-lint run ./...
```

Expected: PASS, and no depguard or import-cycle complaint.

- [ ] **Step 7: Commit**

```bash
git add hdms-backend/internal/platform/staffauth hdms-backend/internal/platform/auth/middleware.go hdms-backend/cmd/hdms-api/main.go hdms-backend/test/integration
git commit -m "feat(phase-7b): staff middleware with scope separation from the admin realm"
```

---

### Task 5: The Entra OIDC client and the PKCE state store

**Files:**
- Create: `hdms-backend/internal/platform/staffauth/oidc.go`
- Modify: `hdms-backend/internal/platform/config/config.go` (five new variables)
- Modify: `hdms-backend/go.mod`, `go.sum` (new dependency)
- Modify: `.env.example`
- Test: `hdms-backend/internal/platform/staffauth/oidc_test.go`

**Interfaces:**
- Consumes: `staffauthstore.CreateLoginState`, `ConsumeLoginState`; `cfg.Entra*`.
- Produces:

```go
type EntraConfig struct {
	TenantID, ClientID, ClientSecret, RedirectURL string
	AllowedEmailDomains []string
}
type Claims struct {
	Subject, TenantID, Email, DisplayName, EmployeeNo string
}
func NewOIDC(ctx context.Context, cfg EntraConfig, pool *db.Pool, stateKey []byte) (*OIDC, error)
func (o *OIDC) AuthCodeURL(ctx context.Context, redirectTo string) (string, error)
func (o *OIDC) Exchange(ctx context.Context, state, code string) (Claims, error)
var ErrStateUnknown, ErrTenantNotAllowed, ErrDomainNotAllowed error
```

- [ ] **Step 1: Add the dependency**

```bash
cd hdms-backend && go get github.com/coreos/go-oidc/v3@latest && go get golang.org/x/oauth2@latest
```

Why a library: discovery, JWKS fetching and rotation, and ID-token signature and claim validation are exactly the places a hand-rolled implementation goes quietly wrong. `go-oidc` is the reference Go implementation and pulls only `go-jose` and `x/oauth2`.

- [ ] **Step 2: Write the failing test**

Create `hdms-backend/internal/platform/staffauth/oidc_test.go`:

```go
package staffauth

import "testing"

func TestClaimsAreRefusedFromAnotherTenant(t *testing.T) {
	cfg := EntraConfig{TenantID: "tenant-ours", AllowedEmailDomains: []string{"hospital.example"}}
	claims := Claims{Subject: "oid-1", TenantID: "tenant-theirs", Email: "person@hospital.example"}

	if err := checkClaimsAllowed(cfg, claims); err == nil {
		t.Fatal("a token from another tenant was accepted")
	}
}

func TestClaimsAreRefusedFromAnUnlistedEmailDomain(t *testing.T) {
	cfg := EntraConfig{TenantID: "tenant-ours", AllowedEmailDomains: []string{"hospital.example"}}
	claims := Claims{Subject: "oid-1", TenantID: "tenant-ours", Email: "person@elsewhere.example"}

	if err := checkClaimsAllowed(cfg, claims); err == nil {
		t.Fatal("a token with an unlisted email domain was accepted")
	}
}

func TestClaimsFromTheConfiguredTenantAndDomainAreAccepted(t *testing.T) {
	cfg := EntraConfig{TenantID: "tenant-ours", AllowedEmailDomains: []string{"hospital.example"}}
	claims := Claims{Subject: "oid-1", TenantID: "tenant-ours", Email: "Person@Hospital.Example"}

	if err := checkClaimsAllowed(cfg, claims); err != nil {
		t.Fatalf("checkClaimsAllowed = %v, want nil — the domain match is case-insensitive", err)
	}
}

func TestAnEmptyAllowedDomainListAcceptsAnyDomainInTheTenant(t *testing.T) {
	cfg := EntraConfig{TenantID: "tenant-ours"}
	claims := Claims{Subject: "oid-1", TenantID: "tenant-ours", Email: "person@anywhere.example"}

	if err := checkClaimsAllowed(cfg, claims); err != nil {
		t.Fatalf("checkClaimsAllowed = %v, want nil — tenant membership alone is the gate when no domains are listed", err)
	}
}
```

- [ ] **Step 3: Run it and watch it fail**

```bash
cd hdms-backend && go test ./internal/platform/staffauth/ -run TestClaims -v
```

Expected: FAIL — `undefined: checkClaimsAllowed`.

- [ ] **Step 4: Write the OIDC client**

Create `hdms-backend/internal/platform/staffauth/oidc.go`:

```go
package staffauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/pgtypeconv"
	staffauthstore "github.com/hito-hospital/hdms/internal/platform/staffauth/store"
	"golang.org/x/oauth2"
)

const loginStateTTL = 10 * time.Minute

var (
	ErrStateUnknown     = errors.New("staffauth: unknown, expired or already-used login state")
	ErrTenantNotAllowed = errors.New("staffauth: sign-in is restricted to the hospital tenant")
	ErrDomainNotAllowed = errors.New("staffauth: sign-in is restricted to the configured email domains")
)

// EntraConfig is the hospital's app registration. AllowedEmailDomains may be
// empty, in which case tenant membership alone is the gate (ADR-0012).
type EntraConfig struct {
	TenantID            string
	ClientID            string
	ClientSecret        string
	RedirectURL         string
	AllowedEmailDomains []string
}

// Claims is the subset of the ID token this system cares about. Everything
// else Entra sends is ignored on purpose.
type Claims struct {
	Subject     string
	TenantID    string
	Email       string
	DisplayName string
	EmployeeNo  string
}

type OIDC struct {
	cfg      EntraConfig
	verifier *oidc.IDTokenVerifier
	oauth    oauth2.Config
	pool     *db.Pool
	stateKey []byte
}

// NewOIDC performs discovery once at start-up. A failure here must not stop
// the process: password login has to keep working when Entra is unreachable,
// so the caller logs and leaves Microsoft sign-in disabled.
func NewOIDC(ctx context.Context, cfg EntraConfig, pool *db.Pool, stateKey []byte) (*OIDC, error) {
	issuer := fmt.Sprintf("https://login.microsoftonline.com/%s/v2.0", cfg.TenantID)
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("staffauth: oidc discovery: %w", err)
	}
	return &OIDC{
		cfg:      cfg,
		verifier: provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
		oauth: oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Endpoint:     provider.Endpoint(),
			Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
		},
		pool:     pool,
		stateKey: stateKey,
	}, nil
}

// AuthCodeURL mints a state and a PKCE verifier, stores them server-side, and
// returns the URL to redirect the browser to. The verifier never reaches the
// browser, so a stolen redirect cannot complete the exchange.
func (o *OIDC) AuthCodeURL(ctx context.Context, redirectTo string) (string, error) {
	state, err := randomToken()
	if err != nil {
		return "", err
	}
	verifier := oauth2.GenerateVerifier()

	enc, err := encryptState(verifier, o.stateKey)
	if err != nil {
		return "", err
	}
	q := staffauthstore.New(db.Conn(ctx, o.pool))
	if err := q.CreateLoginState(ctx, staffauthstore.CreateLoginStateParams{
		StateHash:   hashToken(state),
		VerifierEnc: enc,
		RedirectTo:  pgtypeconv.Text(redirectTo),
		ExpiresAt:   pgtypeconv.Timestamptz(time.Now().Add(loginStateTTL)),
	}); err != nil {
		return "", fmt.Errorf("staffauth: store login state: %w", err)
	}

	return o.oauth.AuthCodeURL(state,
		oauth2.AccessTypeOnline,
		oauth2.S256ChallengeOption(verifier),
	), nil
}

// Exchange consumes the state, redeems the code, verifies the ID token and
// returns the claims this system uses. The state row is consumed atomically,
// so a replayed callback fails.
func (o *OIDC) Exchange(ctx context.Context, state, code string) (Claims, error) {
	q := staffauthstore.New(db.Conn(ctx, o.pool))
	row, err := q.ConsumeLoginState(ctx, hashToken(state))
	if err != nil {
		return Claims{}, ErrStateUnknown
	}
	verifier, err := decryptState(row.VerifierEnc, o.stateKey)
	if err != nil {
		return Claims{}, fmt.Errorf("staffauth: decrypt login state: %w", err)
	}

	token, err := o.oauth.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return Claims{}, fmt.Errorf("staffauth: exchange code: %w", err)
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		return Claims{}, errors.New("staffauth: no id_token in the token response")
	}
	idToken, err := o.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return Claims{}, fmt.Errorf("staffauth: verify id token: %w", err)
	}

	var raw struct {
		OID               string `json:"oid"`
		TID               string `json:"tid"`
		Email             string `json:"email"`
		PreferredUsername string `json:"preferred_username"`
		Name              string `json:"name"`
		EmployeeID        string `json:"employeeId"`
		OnPremSAM         string `json:"onpremisessamaccountname"`
	}
	if err := idToken.Claims(&raw); err != nil {
		return Claims{}, fmt.Errorf("staffauth: read claims: %w", err)
	}

	email := raw.Email
	if email == "" {
		email = raw.PreferredUsername
	}
	claims := Claims{
		Subject:     raw.OID,
		TenantID:    raw.TID,
		Email:       email,
		DisplayName: raw.Name,
		EmployeeNo:  raw.EmployeeID,
	}
	if err := checkClaimsAllowed(o.cfg, claims); err != nil {
		return Claims{}, err
	}
	return claims, nil
}

// checkClaimsAllowed is the gate that replaced the administrator (ADR-0012):
// the right tenant, and — when the deployment lists them — an allowed email
// domain. It is a pure function so the policy is testable without a network.
func checkClaimsAllowed(cfg EntraConfig, claims Claims) error {
	if claims.Subject == "" {
		return errors.New("staffauth: the id token carried no subject")
	}
	if cfg.TenantID != "" && !strings.EqualFold(claims.TenantID, cfg.TenantID) {
		return ErrTenantNotAllowed
	}
	if len(cfg.AllowedEmailDomains) == 0 {
		return nil
	}
	_, domain, found := strings.Cut(claims.Email, "@")
	if !found {
		return ErrDomainNotAllowed
	}
	for _, allowed := range cfg.AllowedEmailDomains {
		if strings.EqualFold(strings.TrimSpace(allowed), domain) {
			return nil
		}
	}
	return ErrDomainNotAllowed
}

func encryptState(verifier string, key []byte) ([]byte, error) {
	return aesGCMSeal([]byte(verifier), key)
}

func decryptState(ciphertext, key []byte) (string, error) {
	plain, err := aesGCMOpen(ciphertext, key)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
```

Add `aesGCMSeal`/`aesGCMOpen` in the same file, modelled on
`internal/modules/credentials/internal/cryptox/cryptox.go` (read it and mirror
its nonce handling exactly rather than writing a second scheme):

```go
func aesGCMSeal(plain, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("staffauth: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("staffauth: new gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("staffauth: read nonce: %w", err)
	}
	return gcm.Seal(nonce, nonce, plain, nil), nil
}

func aesGCMOpen(ciphertext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("staffauth: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("staffauth: new gcm: %w", err)
	}
	if len(ciphertext) < gcm.NonceSize() {
		return nil, errors.New("staffauth: ciphertext too short")
	}
	nonce, body := ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():]
	return gcm.Open(nil, nonce, body, nil)
}
```

Unused imports (`base64`, `sha256`) come from `session.go` in the same package — remove any this file does not use.

`checkClaimsAllowed` runs inside `Exchange`, before `Exchange` returns — so a
token from another tenant or an unlisted domain is refused before
`resolveOrProvision` is ever reached and before a single row is written. That
ordering is the whole gate; do not move the check into the handler.

- [ ] **Step 5: Add configuration**

In `hdms-backend/internal/platform/config/config.go`, following the existing helpers:

```go
	cfg.EntraTenantID = os.Getenv("HDMS_ENTRA_TENANT_ID")
	cfg.EntraClientID = os.Getenv("HDMS_ENTRA_CLIENT_ID")
	cfg.EntraClientSecret = os.Getenv("HDMS_ENTRA_CLIENT_SECRET")
	cfg.EntraRedirectURL = getenvDefault("HDMS_ENTRA_REDIRECT_URL", "https://localhost:8443/v1/staff/auth/microsoft/callback")
	cfg.EntraAllowedDomains = splitAndTrim(os.Getenv("HDMS_ENTRA_ALLOWED_EMAIL_DOMAINS"))
	cfg.StaffSessionTTL = getenvDurationDefault("HDMS_STAFF_SESSION_TTL", 12*time.Hour, &errs)
```

None of these are required: with no tenant configured, Microsoft sign-in is off and password login still works. Add the matching block to `.env.example` with empty values and a comment saying exactly that.

- [ ] **Step 6: Run the tests and the linter**

```bash
cd hdms-backend && go test ./internal/platform/staffauth/... -v && golangci-lint run ./internal/platform/staffauth/...
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add hdms-backend/internal/platform/staffauth hdms-backend/internal/platform/config hdms-backend/go.mod hdms-backend/go.sum .env.example
git commit -m "feat(phase-7c): Entra OIDC client with server-side PKCE state"
```

---

### Task 6: Sign-on orchestration — the linking ladder, provisioning and completion

This is the task that makes self-signup real. It lives in `apiserver` because it spans identity, credentials and staffauth, exactly as `RegisterWithCard` does.

**Files:**
- Modify: `hdms-backend/api/openapi.yaml` (new paths and schemas; bump version to `1.2.0`)
- Create: `hdms-backend/internal/apiserver/staff_auth.go`
- Create: `hdms-backend/internal/apiserver/staff_signon.go`
- Modify: `hdms-backend/internal/apiserver/server.go` (two new fields on `Server` and `New`)
- Test: `hdms-backend/test/integration/staff_signon_test.go`

**Interfaces:**
- Consumes: `staffauth.Service`, `staffauth.OIDC`, `identity.Service`, `credentials.Service`.
- Produces:

```go
// apiserver
func (s *Server) StaffPasswordLogin(w http.ResponseWriter, r *http.Request)
func (s *Server) StaffMicrosoftStart(w http.ResponseWriter, r *http.Request)
func (s *Server) StaffMicrosoftCallback(w http.ResponseWriter, r *http.Request)
func (s *Server) StaffLogout(w http.ResponseWriter, r *http.Request)
func (s *Server) GetStaffMe(w http.ResponseWriter, r *http.Request)
func (s *Server) CompleteStaffProfile(w http.ResponseWriter, r *http.Request)
func (s *Server) ChangeStaffPassword(w http.ResponseWriter, r *http.Request)
// the ladder, unexported and unit-testable through the HTTP surface:
func (s *Server) resolveOrProvision(ctx context.Context, claims staffauth.Claims) (identityapi.UserSummary, staffauth.Account, error)
```

- [ ] **Step 1: Add the contract**

In `hdms-backend/api/openapi.yaml`, bump `info.version` to `"1.2.0"`, extend the description with a line saying v1.2.0 adds the staff realm and is additive only, and add:

```yaml
  /staff/auth/password:
    post:
      operationId: staffPasswordLogin
      summary: Staff login with employee number and password. Sets the staff session cookies.
      tags: [staff]
      security: []
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: "#/components/schemas/StaffPasswordLoginRequest"
      responses:
        "200":
          description: Authenticated. `Set-Cookie` carries `hdms_staff_session` and `hdms_staff_csrf`.
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/StaffMe"
        default:
          description: Invalid credentials, a locked account, or a suspended user.
          content:
            application/problem+json:
              schema:
                $ref: "#/components/schemas/Problem"

  /staff/auth/microsoft/start:
    get:
      operationId: staffMicrosoftStart
      summary: Begin Microsoft sign-in. Redirects to Entra.
      tags: [staff]
      security: []
      responses:
        "302":
          description: Redirect to the identity provider.
        default:
          description: Microsoft sign-in is not configured for this deployment.
          content:
            application/problem+json:
              schema:
                $ref: "#/components/schemas/Problem"

  /staff/auth/microsoft/callback:
    get:
      operationId: staffMicrosoftCallback
      summary: Entra redirect target. Links or provisions the user and sets the staff session cookies.
      tags: [staff]
      security: []
      parameters:
        - {name: code, in: query, required: false, schema: {type: string}}
        - {name: state, in: query, required: false, schema: {type: string}}
        - {name: error, in: query, required: false, schema: {type: string}}
      responses:
        "302":
          description: Redirect back into the staff app, signed in or with an error code.
        default:
          description: The state was unknown, or the tenant or domain is not allowed.
          content:
            application/problem+json:
              schema:
                $ref: "#/components/schemas/Problem"

  /staff/auth/logout:
    post:
      operationId: staffLogout
      summary: End the staff session.
      tags: [staff]
      responses:
        "204": {description: Logged out.}
        default:
          description: Error.
          content:
            application/problem+json:
              schema: {$ref: "#/components/schemas/Problem"}

  /staff/me:
    get:
      operationId: getStaffMe
      summary: The signed-in staff member, their status, and what the app must ask them for next.
      tags: [staff]
      responses:
        "200":
          description: The current staff member.
          content:
            application/json:
              schema: {$ref: "#/components/schemas/StaffMe"}
        default:
          description: Error.
          content:
            application/problem+json:
              schema: {$ref: "#/components/schemas/Problem"}

  /staff/me/profile:
    post:
      operationId: completeStaffProfile
      summary: One-time completion of a self-signed-up profile. Sets the employee number and mints the QR.
      tags: [staff]
      requestBody:
        required: true
        content:
          application/json:
            schema: {$ref: "#/components/schemas/CompleteStaffProfileRequest"}
      responses:
        "200":
          description: Profile completed.
          content:
            application/json:
              schema: {$ref: "#/components/schemas/StaffMe"}
        default:
          description: The employee number is taken or invalid.
          content:
            application/problem+json:
              schema: {$ref: "#/components/schemas/Problem"}

  /staff/me/password:
    post:
      operationId: changeStaffPassword
      summary: Change the signed-in staff member's own password.
      tags: [staff]
      requestBody:
        required: true
        content:
          application/json:
            schema: {$ref: "#/components/schemas/ChangeStaffPasswordRequest"}
      responses:
        "204": {description: Changed.}
        default:
          description: The current password was wrong, or the new one is too short.
          content:
            application/problem+json:
              schema: {$ref: "#/components/schemas/Problem"}
```

And under `components.schemas`:

```yaml
    StaffPasswordLoginRequest:
      type: object
      required: [employeeNo, password]
      properties:
        employeeNo: {type: string, maxLength: 32}
        password: {type: string, maxLength: 256}

    ChangeStaffPasswordRequest:
      type: object
      required: [currentPassword, newPassword]
      properties:
        currentPassword: {type: string, maxLength: 256}
        newPassword: {type: string, minLength: 12, maxLength: 256}

    CompleteStaffProfileRequest:
      type: object
      required: [employeeNo]
      properties:
        employeeNo: {type: string, maxLength: 32}
        departmentId: {type: string, format: uuid}

    StaffMe:
      type: object
      required: [userId, employeeNo, fullName, status, profileComplete, mustChangePassword, hasPassword, signInMethods]
      properties:
        userId: {type: string, format: uuid}
        employeeNo: {type: string}
        fullName: {type: string}
        email: {type: string}
        departmentName: {type: string}
        status: {type: string, enum: [active, suspended, archived]}
        profileComplete: {type: boolean}
        mustChangePassword: {type: boolean}
        hasPassword: {type: boolean}
        signInMethods:
          type: array
          items: {type: string, enum: [password, microsoft]}
```

Then:

```bash
task generate && cd hdms-backend && go build ./...
```

Expected: `gen.ServerInterface` now requires the seven new methods, so the build fails until Step 3. That failure is the test for this step.

- [ ] **Step 2: Write the failing integration test**

Create `hdms-backend/test/integration/staff_signon_test.go`:

```go
//go:build integration

package integration

import (
	"context"
	"sync"
	"testing"

	"github.com/hito-hospital/hdms/internal/platform/staffauth"
)

// The ladder: an existing link wins, then the employee-number claim, then the
// email, and only then is a user provisioned.
func TestMicrosoftSignInLinksToAnExistingUserByEmployeeNumber(t *testing.T) {
	env := newHTTPTestEnv(t)
	ctx := context.Background()

	existing := env.SeedUser(t, "E-LINK-1", "Existing Person", "existing@hospital.example")

	user, account, err := env.Server.ResolveOrProvisionForTest(ctx, staffauth.Claims{
		Subject: "oid-link-1", TenantID: env.TenantID,
		Email: "someone.else@hospital.example", DisplayName: "Existing Person",
		EmployeeNo: "E-LINK-1",
	})
	if err != nil {
		t.Fatalf("resolveOrProvision: %v", err)
	}
	if user.ID != existing.ID {
		t.Fatalf("linked to user %s, want the existing %s", user.ID, existing.ID)
	}
	if !account.ProfileComplete {
		t.Fatal("linking to an existing user must leave the profile complete")
	}
}

func TestMicrosoftSignInLinksToAnExistingUserByEmail(t *testing.T) {
	env := newHTTPTestEnv(t)
	ctx := context.Background()

	existing := env.SeedUser(t, "E-LINK-2", "Email Person", "email.person@hospital.example")

	user, _, err := env.Server.ResolveOrProvisionForTest(ctx, staffauth.Claims{
		Subject: "oid-link-2", TenantID: env.TenantID,
		Email: "EMAIL.PERSON@hospital.example", DisplayName: "Email Person",
	})
	if err != nil {
		t.Fatalf("resolveOrProvision: %v", err)
	}
	if user.ID != existing.ID {
		t.Fatalf("linked to user %s, want the existing %s — the email match is case-insensitive", user.ID, existing.ID)
	}
}

func TestMicrosoftCallbackProvisionsAUserExactlyOnce(t *testing.T) {
	env := newHTTPTestEnv(t)
	ctx := context.Background()
	claims := staffauth.Claims{
		Subject: "oid-race", TenantID: env.TenantID,
		Email: "race@hospital.example", DisplayName: "Race Person",
	}

	var wg sync.WaitGroup
	ids := make([]string, 4)
	errs := make([]error, 4)
	for i := range ids {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			user, _, err := env.Server.ResolveOrProvisionForTest(ctx, claims)
			ids[i], errs[i] = user.ID, err
		}(i)
	}
	wg.Wait()

	seen := map[string]bool{}
	for i := range ids {
		if errs[i] != nil {
			continue // one loser in a race is acceptable; two users are not
		}
		seen[ids[i]] = true
	}
	if len(seen) != 1 {
		t.Fatalf("concurrent callbacks produced %d distinct users, want exactly 1", len(seen))
	}
}

func TestAProvisionedUserWithNoEmployeeNumberClaimHasNoCredentialUntilCompletion(t *testing.T) {
	env := newHTTPTestEnv(t)
	ctx := context.Background()

	user, account, err := env.Server.ResolveOrProvisionForTest(ctx, staffauth.Claims{
		Subject: "oid-incomplete", TenantID: env.TenantID,
		Email: "incomplete@hospital.example", DisplayName: "Incomplete Person",
	})
	if err != nil {
		t.Fatalf("resolveOrProvision: %v", err)
	}
	if account.ProfileComplete {
		t.Fatal("a provisioning with no employee-number claim must leave the profile incomplete")
	}
	if n := env.CountActiveCredentials(t, user.ID); n != 0 {
		t.Fatalf("active credentials = %d, want 0 until the profile is complete", n)
	}
}
```

Add `SeedUser`, `CountActiveCredentials` and `TenantID` to the existing test env helper, and expose the ladder for testing with a thin wrapper next to the handler:

```go
// ResolveOrProvisionForTest exposes the sign-on ladder to integration tests.
// Production code calls the unexported resolveOrProvision through the callback
// handler; this wrapper exists so the ladder's rungs can be tested one at a
// time without standing up a fake identity provider.
func (s *Server) ResolveOrProvisionForTest(ctx context.Context, claims staffauth.Claims) (identityapi.UserSummary, staffauth.Account, error) {
	return s.resolveOrProvision(ctx, claims)
}
```

Add one more test in the same file, because a suspended user reaching the app
would be worse than a locked-out one:

```go
func TestASuspendedUserCannotSignIn(t *testing.T) {
	env := newHTTPTestEnv(t)
	ctx := context.Background()

	user := env.SeedUser(t, "E-SUSP-1", "Suspended Person", "suspended@hospital.example")
	account := env.SeedStaffAccountWithPassword(t, user.ID, "a-long-enough-password")
	_ = account
	if _, err := env.Identity.SuspendUser(ctx, user.ID, "left the ward", "admin:test"); err != nil {
		t.Fatalf("SuspendUser: %v", err)
	}

	body := strings.NewReader(`{"employeeNo":"E-SUSP-1","password":"a-long-enough-password"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/staff/auth/password", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	env.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("login as a suspended user = %d, want 401", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "suspend") {
		t.Fatalf("the refusal explained that the account is suspended: %s", rec.Body.String())
	}
}
```

The second assertion is the point: a suspended user and a wrong password must
be indistinguishable from outside, or the endpoint becomes a way to ask
whether someone still works here.

- [ ] **Step 3: Write the ladder**

Create `hdms-backend/internal/apiserver/staff_signon.go`:

```go
package apiserver

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hito-hospital/hdms/internal/modules/credentials/credentialsapi"
	"github.com/hito-hospital/hdms/internal/modules/identity/identityapi"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/staffauth"
)

const selfSignupActor = "self:microsoft"

// resolveOrProvision is the sign-on ladder from the Phase 7 design: an
// existing identity link wins, then a matching employee number, then a
// matching email, and only then is a user provisioned. Every rung runs in one
// transaction so a racing second callback either sees the first one's rows or
// loses on a unique constraint — never produces a duplicate person.
func (s *Server) resolveOrProvision(ctx context.Context, claims staffauth.Claims) (identityapi.UserSummary, staffauth.Account, error) {
	var user identityapi.UserSummary
	var account staffauth.Account

	err := db.NewTxManager(s.pool).Do(ctx, func(ctx context.Context) error {
		// Rung 1 — this Entra identity has signed in before.
		if linked, err := s.staffAuth.AccountForIdentity(ctx, "microsoft", claims.Subject); err == nil {
			found, err := s.identity.LookupUser(ctx, linked.UserID)
			if err != nil {
				return err
			}
			user, account = found, linked
			return nil
		} else if !errors.Is(err, staffauth.ErrAccountNotFound) {
			return err
		}

		// Rung 2 — the token names an employee number we already hold.
		if claims.EmployeeNo != "" {
			if found, err := s.identity.LookupUserByEmployeeNo(ctx, claims.EmployeeNo); err == nil {
				return s.linkTo(ctx, found, claims, &user, &account)
			} else if !errors.Is(err, identityapi.ErrUserNotFound) {
				return err
			}
		}

		// Rung 3 — the verified email belongs to a live user.
		if claims.Email != "" {
			if found, err := s.identity.LookupUserByEmail(ctx, claims.Email); err == nil {
				return s.linkTo(ctx, found, claims, &user, &account)
			} else if !errors.Is(err, identityapi.ErrUserNotFound) {
				return err
			}
		}

		// Rung 4 — provision. When Entra supplies no employee number the
		// profile stays incomplete and no credential is minted, so an
		// unfinished account can never scan at a kiosk.
		employeeNo := strings.TrimSpace(claims.EmployeeNo)
		complete := employeeNo != ""
		if !complete {
			employeeNo = placeholderEmployeeNo(claims.Subject)
		}
		created, err := s.identity.CreateUser(ctx, identityapi.CreateUserParams{
			EmployeeNo:   employeeNo,
			FullName:     fallbackName(claims),
			Email:        claims.Email,
			RegisteredBy: selfSignupActor,
		})
		if err != nil {
			return err
		}
		newAccount, err := s.staffAuth.EnsureAccount(ctx, created.ID, selfSignupActor, complete)
		if err != nil {
			return err
		}
		if err := s.staffAuth.LinkIdentity(ctx, newAccount.ID, "microsoft", claims.Subject, claims.TenantID, claims.Email); err != nil {
			return err
		}
		if complete {
			if _, err := s.credentials.Issue(ctx, credentialsapi.IssueParams{
				SubjectType: credentialsapi.SubjectUser,
				SubjectID:   created.ID,
				Kind:        credentialsapi.KindQR,
				IssuedBy:    selfSignupActor,
			}); err != nil {
				return err
			}
		}
		user, account = created, newAccount
		return nil
	})
	if err != nil {
		return identityapi.UserSummary{}, staffauth.Account{}, err
	}
	return user, account, nil
}

// linkTo attaches this Entra identity to a user who already exists. It never
// mints a credential: an existing user either has a card already or an
// administrator will issue one.
func (s *Server) linkTo(ctx context.Context, found identityapi.UserSummary, claims staffauth.Claims,
	user *identityapi.UserSummary, account *staffauth.Account) error {
	acct, err := s.staffAuth.EnsureAccount(ctx, found.ID, selfSignupActor, true)
	if err != nil {
		return err
	}
	if err := s.staffAuth.LinkIdentity(ctx, acct.ID, "microsoft", claims.Subject, claims.TenantID, claims.Email); err != nil {
		return err
	}
	*user, *account = found, acct
	return nil
}

// placeholderEmployeeNo produces a value that satisfies the employee-number
// format rules and is obviously provisional to an administrator reading the
// users list. It is replaced the first time the person opens the app.
func placeholderEmployeeNo(subject string) string {
	trimmed := strings.ReplaceAll(subject, "-", "")
	if len(trimmed) > 8 {
		trimmed = trimmed[:8]
	}
	return fmt.Sprintf("MS-%s", strings.ToUpper(trimmed))
}

func fallbackName(claims staffauth.Claims) string {
	if name := strings.TrimSpace(claims.DisplayName); name != "" {
		return name
	}
	if local, _, found := strings.Cut(claims.Email, "@"); found && local != "" {
		return local
	}
	return "Unnamed staff member"
}
```

This needs three additions elsewhere, each small:

1. `staffauth.AccountForIdentity(ctx, provider, subject) (Account, error)` and
   `staffauth.LinkIdentity(ctx, accountID, provider, subject, tenantID, email string) error`,
   built on `GetStaffIdentity` and `LinkStaffIdentity` from Task 2, returning
   `ErrAccountNotFound` on `pgx.ErrNoRows`.
2. `identity.LookupUserByEmail(ctx, email) (UserSummary, error)` plus a
   `GetUserByEmail` query — `SELECT * FROM users WHERE lower(email) = lower($1) AND status <> 'archived'` —
   and its entry in `identityapi.Service`.
3. `identityapi.ErrUserNotFound` already exists; confirm the name with
   `grep -n "ErrUserNotFound" internal/modules/identity/identityapi/*.go` and use whatever is there.

- [ ] **Step 4: Write the handlers**

Create `hdms-backend/internal/apiserver/staff_auth.go` with the seven `gen.ServerInterface` methods. The shapes that matter:

```go
func (s *Server) StaffPasswordLogin(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[gen.StaffPasswordLoginRequest](w, r)
	if !ok {
		return
	}

	// An unknown employee number and a wrong password answer identically, so
	// the endpoint cannot be used to enumerate staff.
	user, err := s.identity.LookupUserByEmployeeNo(r.Context(), req.EmployeeNo)
	if err != nil {
		writeStaffLoginRefusal(w, r)
		return
	}
	account, err := s.staffAuth.AccountForUser(r.Context(), user.ID)
	if err != nil {
		writeStaffLoginRefusal(w, r)
		return
	}
	if user.Status != identityapi.StatusActive {
		writeStaffLoginRefusal(w, r)
		return
	}
	if err := s.staffAuth.VerifyPassword(r.Context(), account.ID, req.Password); err != nil {
		s.recordStaffLoginFailure(r.Context(), r, user.ID, err)
		writeStaffLoginRefusal(w, r)
		return
	}

	sessionToken, csrfToken, err := s.staffAuth.StartSession(r.Context(), account.ID)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	ttl := int(s.staffAuth.SessionTTL().Seconds())
	http.SetCookie(w, staffauth.SessionCookie(sessionToken, ttl))
	http.SetCookie(w, staffauth.CSRFCookie(csrfToken, ttl))

	s.recordStaffAudit(r.Context(), user.ID, "staff.login", map[string]any{"method": "password"})
	me, err := s.staffMe(r.Context(), user, account)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, me)
}
```

`StaffMicrosoftStart` answers 503 with a `Problem` when `s.staffOIDC == nil`, otherwise `http.Redirect(w, r, url, http.StatusFound)`.

`StaffMicrosoftCallback` exchanges, calls `resolveOrProvision`, starts a session, sets the two cookies, audits `staff.signup` for a fresh user or `staff.login` otherwise, and redirects to `/` in the staff app — or to `/?error=<code>` for a refusal, so the app can show a translated message rather than a raw problem document.

`CompleteStaffProfile` validates the employee number through `identityapi.ValidateEmployeeNo`, calls `identity.UpdateUser`, `staffAuth.MarkProfileComplete`, and then issues the QR with `credentials.Issue`, all inside one `db.NewTxManager(s.pool).Do`.

`ChangeStaffPassword` calls `staffAuth.ChangeOwnPassword` and then `staffAuth.RevokeAllSessions` for every session but the current one — simplest correct version: revoke all, then start a fresh session and reset the cookies.

- [ ] **Step 5: Wire the services into the server**

Add `staffAuth *staffauth.Service` and `staffOIDC *staffauth.OIDC` to `Server` and to `New(...)`, and construct both in `cmd/hdms-api/main.go`. When `cfg.EntraTenantID == ""`, pass a nil `*staffauth.OIDC` and log once that Microsoft sign-in is disabled.

- [ ] **Step 6: Rate-limit the password endpoint**

Per-account lockout (Task 3) stops an attack on one account; it does nothing
against one attempt each against a thousand accounts. Add a per-IP limiter in
front of `POST /v1/staff/auth/password` using `golang.org/x/time/rate`, which
is already a direct dependency:

```go
// staffLoginLimiter allows a burst of 10 attempts per client address and
// refills at one every six seconds. Lockout protects an account; this
// protects the whole staff roster from being sprayed.
type staffLoginLimiter struct {
	mu       sync.Mutex
	byClient map[string]*rate.Limiter
}

func (l *staffLoginLimiter) allow(clientIP string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	lim, ok := l.byClient[clientIP]
	if !ok {
		lim = rate.NewLimiter(rate.Every(6*time.Second), 10)
		l.byClient[clientIP] = lim
	}
	return lim.Allow()
}
```

Refuse with 429 and a `Problem` of type `too-many-attempts`. Test it:

```go
func TestPasswordLoginIsRateLimitedPerClient(t *testing.T) {
	env := newHTTPTestEnv(t)

	var lastCode int
	for i := 0; i < 30; i++ {
		body := strings.NewReader(`{"employeeNo":"E-NOBODY","password":"wrong-password"}`)
		req := httptest.NewRequest(http.MethodPost, "/v1/staff/auth/password", body)
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "203.0.113.7:54321"
		rec := httptest.NewRecorder()
		env.Handler.ServeHTTP(rec, req)
		lastCode = rec.Code
	}
	if lastCode != http.StatusTooManyRequests {
		t.Fatalf("after 30 rapid attempts from one address the response was %d, want 429", lastCode)
	}
}
```

- [ ] **Step 7: Run everything**

```bash
cd hdms-backend && go build ./... && go test ./... && go test -race -tags=integration ./test/... && golangci-lint run ./...
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add hdms-backend/api/openapi.yaml hdms-backend/internal hdms-backend/test hdms-frontend/packages/api-client/src
git commit -m "feat(phase-7c): staff sign-on, the linking ladder and self-provisioning"
```

---

### Task 7: Credential reveal for user cards

**Files:**
- Modify: `hdms-backend/internal/modules/credentials/module.go:105-112` (encrypt user tokens) and add a `Reveal` method
- Modify: `hdms-backend/internal/platform/auth/roles.go` (classify the new path as `admin`)
- Modify: `hdms-backend/api/openapi.yaml`
- Create: `hdms-backend/internal/apiserver/credentials_reveal.go`
- Test: `hdms-backend/test/integration/credentials_reveal_test.go`

**Interfaces:**
- Consumes: `cryptox.Encrypt`/`Decrypt`, `credentialsstore`, `auditapi`.
- Produces:

```go
func (s *Service) Reveal(ctx context.Context, credentialID, actor string) (credentialsapi.IssuedCredential, error)
var credentialsapi.ErrTokenNotRecoverable = errors.New("credentials: this credential predates reversible storage")
// POST /v1/credentials/{id}/reveal → RevealResponse{token, credential}
```

- [ ] **Step 1: Write the failing test**

Create `hdms-backend/test/integration/credentials_reveal_test.go`:

```go
//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/hito-hospital/hdms/internal/modules/credentials/credentialsapi"
)

func TestAUserCardTokenCanBeRevealedAfterIssue(t *testing.T) {
	env := newCredentialsTestEnv(t)
	ctx := context.Background()

	user := env.SeedUser(t, "E-REVEAL-1", "Reveal Person", "reveal@hospital.example")
	issued, err := env.Credentials.Issue(ctx, credentialsapi.IssueParams{
		SubjectType: credentialsapi.SubjectUser, SubjectID: user.ID,
		Kind: credentialsapi.KindQR, IssuedBy: "admin:test",
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	revealed, err := env.Credentials.Reveal(ctx, issued.Credential.ID, "admin:test")
	if err != nil {
		t.Fatalf("Reveal: %v", err)
	}
	if revealed.Token != issued.Token {
		t.Fatalf("revealed token = %q, want the issued token %q — reveal must not mint a new one", revealed.Token, issued.Token)
	}
}

func TestEveryRevealWritesACredentialEvent(t *testing.T) {
	env := newCredentialsTestEnv(t)
	ctx := context.Background()

	user := env.SeedUser(t, "E-REVEAL-2", "Audited Person", "audited@hospital.example")
	issued, err := env.Credentials.Issue(ctx, credentialsapi.IssueParams{
		SubjectType: credentialsapi.SubjectUser, SubjectID: user.ID,
		Kind: credentialsapi.KindQR, IssuedBy: "admin:test",
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := env.Credentials.Reveal(ctx, issued.Credential.ID, "admin:alice"); err != nil {
		t.Fatalf("Reveal: %v", err)
	}

	var count int
	if err := env.Pool.QueryRow(ctx,
		`SELECT count(*) FROM credential_events WHERE credential_id = $1 AND kind = 'revealed' AND actor = 'admin:alice'`,
		issued.Credential.ID).Scan(&count); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if count != 1 {
		t.Fatalf("revealed events = %d, want 1", count)
	}
}

func TestRevealingARevokedCredentialIsRefused(t *testing.T) {
	env := newCredentialsTestEnv(t)
	ctx := context.Background()

	user := env.SeedUser(t, "E-REVEAL-3", "Revoked Person", "revoked@hospital.example")
	issued, err := env.Credentials.Issue(ctx, credentialsapi.IssueParams{
		SubjectType: credentialsapi.SubjectUser, SubjectID: user.ID,
		Kind: credentialsapi.KindQR, IssuedBy: "admin:test",
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := env.Credentials.Revoke(ctx, issued.Credential.ID, "lost", "admin:test"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, err := env.Credentials.Reveal(ctx, issued.Credential.ID, "admin:test"); err == nil {
		t.Fatal("revealing a revoked credential succeeded; a dead card must not be printable")
	}
}
```

Add the role test too — the reveal being admin-only is the compensating
control ADR-0016 leans on, so it gets its own proof:

```go
func TestRevealIsRefusedForNonAdminRoles(t *testing.T) {
	env := newHTTPTestEnv(t)

	user := env.SeedUser(t, "E-REVEAL-4", "Role Person", "role@hospital.example")
	credentialID := env.SeedActiveUserCard(t, user.ID)

	for _, role := range []string{"technician", "viewer"} {
		session := env.AdminSessionForRole(t, role)
		req := httptest.NewRequest(http.MethodPost, "/v1/credentials/"+credentialID+"/reveal", nil)
		req.AddCookie(&http.Cookie{Name: "hdms_session", Value: session.Token})
		req.Header.Set("X-CSRF-Token", session.CSRFToken)
		rec := httptest.NewRecorder()
		env.Handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("reveal as %s = %d, want 403", role, rec.Code)
		}
	}
}
```

Check `Revoke`'s real signature before writing this — `grep -n "func (s \*Service) Revoke" internal/modules/credentials/module.go` — and match it.

- [ ] **Step 2: Run it and watch it fail**

```bash
cd hdms-backend && go test -race -tags=integration ./test/integration/ -run TestAUserCardTokenCanBeRevealed -v
```

Expected: FAIL — `env.Credentials.Reveal undefined`, and once that compiles, a decrypt failure because user tokens are not yet stored encrypted.

- [ ] **Step 3: Encrypt user tokens at issue**

In `internal/modules/credentials/module.go`, replace the device-only block in `issueOne`:

```go
	// Both subject types are stored reversibly from Phase 7 (ADR-0016): a
	// device sticker was never a secret, and an administrator must be able to
	// print a staff card again without invalidating the one in the wallet.
	// The deployment key is what protects both, which is why reveal is
	// admin-only and audited.
	tokenEnc, err := cryptox.Encrypt(token, s.encKey)
	if err != nil {
		return credentialsapi.IssuedCredential{}, fmt.Errorf("credentials: encrypt token: %w", err)
	}
```

- [ ] **Step 4: Add `Reveal`**

Add to the same file, modelled on `Reprint` but without the device restriction and without touching the print counter:

```go
// Reveal returns the plaintext token of an active credential of either
// subject type. It does not count as a print and does not mint anything: it
// is the administrator looking at a card they are allowed to look at, and it
// is recorded as such.
func (s *Service) Reveal(ctx context.Context, credentialID, actor string) (credentialsapi.IssuedCredential, error) {
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
		if len(current.TokenEnc) == 0 {
			return credentialsapi.ErrTokenNotRecoverable
		}
		token, err := cryptox.Decrypt(current.TokenEnc, s.encKey)
		if err != nil {
			return fmt.Errorf("credentials: decrypt for reveal: %w", err)
		}
		cred := toCredential(current)
		if err := s.insertEvent(ctx, current.ID, "revealed", actor, "", nil); err != nil {
			return err
		}
		if err := s.audit.Record(ctx, auditapi.Event{
			Actor: actor, Action: "credential.revealed", Subject: subjectOrCredential(cred),
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
```

Add `ErrTokenNotRecoverable` to `credentialsapi`, and map it in `writeServiceError` to a 409 `Problem` with type `token-not-recoverable`.

- [ ] **Step 5: Add the endpoint and classify it**

In `api/openapi.yaml`:

```yaml
  /credentials/{id}/reveal:
    post:
      operationId: revealCredential
      summary: Disclose an active credential's plaintext token for display and printing. Administrators only; audited.
      tags: [credentials]
      parameters:
        - {name: id, in: path, required: true, schema: {type: string, format: uuid}}
      responses:
        "200":
          description: The plaintext token.
          content:
            application/json:
              schema: {$ref: "#/components/schemas/IssuedCredential"}
        default:
          description: The credential is not active, or its token predates reversible storage.
          content:
            application/problem+json:
              schema: {$ref: "#/components/schemas/Problem"}
```

Reuse whatever schema `reprintCredential` already returns rather than adding a second shape — check it first.

In `internal/platform/auth/roles.go`, add to the admin-only section:

```go
	"POST /v1/credentials/{id}/reveal": "admin",
```

POST rather than GET on purpose: it must carry the CSRF header, must not be cached, and must never appear in a proxy log's URL.

Create `internal/apiserver/credentials_reveal.go` with the handler calling `s.credentials.Reveal(r.Context(), id, actorFrom(r))`.

- [ ] **Step 6: Run everything**

```bash
task generate && cd hdms-backend && go test ./... && go test -race -tags=integration ./test/... && golangci-lint run ./...
```

Expected: PASS. Existing credential tests that assert `token_enc IS NULL` for user credentials will fail — they encoded the old policy. Update them to assert the new one and note ADR-0016 in the commit.

- [ ] **Step 7: Commit**

```bash
git add hdms-backend hdms-frontend/packages/api-client/src
git commit -m "feat(phase-7d): store user tokens reversibly and add the audited reveal endpoint"
```

---

### Task 8: The staff read endpoints

**Files:**
- Modify: `hdms-backend/api/openapi.yaml`
- Create: `hdms-backend/internal/apiserver/staff_read.go`
- Test: `hdms-backend/test/integration/staff_read_test.go`

**Interfaces:**
- Consumes: `catalog.Service`, `lending.Service`, `credentials.Service`, `staffauth.AccountFromContext`.
- Produces: `GET /v1/staff/devices`, `GET /v1/staff/devices/{id}`, `GET /v1/staff/me/loans`, `GET /v1/staff/me/credential`, and the schema `StaffDevice`:

```yaml
    StaffDevice:
      type: object
      required: [id, assetTag, name, availability]
      properties:
        id: {type: string, format: uuid}
        assetTag: {type: string}
        name: {type: string}
        model: {type: string}
        categoryName: {type: string}
        availability: {type: string, enum: [available, in_use, unavailable]}
        expectedBackAt: {type: string, format: date-time}
```

- [ ] **Step 1: Write the failing test**

Create `hdms-backend/test/integration/staff_read_test.go`:

```go
//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The privacy rule, enforced in the handler and not in the client: a staff
// member learns that a device is out and when it is due, never who has it.
func TestTheStaffDeviceListNeverNamesTheBorrower(t *testing.T) {
	env := newHTTPTestEnv(t)
	borrower := env.SeedUser(t, "E-BORROW-1", "Dr Borrower", "borrower@hospital.example")
	device := env.SeedDevice(t, "AT-STAFF-1", "Projector")
	env.SeedOpenLoan(t, device.ID, borrower.ID)

	req := httptest.NewRequest(http.MethodGet, "/v1/staff/devices", nil)
	req.AddCookie(&http.Cookie{Name: "hdms_staff_session", Value: env.StaffSessionToken})
	rec := httptest.NewRecorder()
	env.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/staff/devices = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "Dr Borrower") || strings.Contains(body, borrower.ID) {
		t.Fatalf("the staff device list disclosed the borrower: %s", body)
	}

	var payload struct {
		Items []struct {
			AssetTag       string `json:"assetTag"`
			Availability   string `json:"availability"`
			ExpectedBackAt string `json:"expectedBackAt"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var found bool
	for _, item := range payload.Items {
		if item.AssetTag == "AT-STAFF-1" {
			found = true
			if item.Availability != "in_use" {
				t.Fatalf("availability = %q, want in_use", item.Availability)
			}
		}
	}
	if !found {
		t.Fatal("the borrowed device is missing from the staff list entirely; it should be listed as in use")
	}
}

func TestAStaffMemberSeesOnlyTheirOwnLoans(t *testing.T) {
	env := newHTTPTestEnv(t)

	mine := env.StaffUser          // the user behind env.StaffSessionToken
	other := env.SeedUser(t, "E-OTHER-1", "Other Person", "other@hospital.example")
	myDevice := env.SeedDevice(t, "AT-MINE-1", "My Laptop")
	theirDevice := env.SeedDevice(t, "AT-THEIRS-1", "Their Laptop")
	env.SeedOpenLoan(t, myDevice.ID, mine.ID)
	env.SeedOpenLoan(t, theirDevice.ID, other.ID)

	req := httptest.NewRequest(http.MethodGet, "/v1/staff/me/loans", nil)
	req.AddCookie(&http.Cookie{Name: "hdms_staff_session", Value: env.StaffSessionToken})
	rec := httptest.NewRecorder()
	env.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/staff/me/loans = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Items []struct {
			DeviceAssetTag string `json:"deviceAssetTag"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Items) != 1 {
		t.Fatalf("loans returned = %d, want exactly 1 — only the signed-in user's own", len(payload.Items))
	}
	if payload.Items[0].DeviceAssetTag != "AT-MINE-1" {
		t.Fatalf("loan for %q, want AT-MINE-1 — another user's loan leaked", payload.Items[0].DeviceAssetTag)
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

```bash
cd hdms-backend && go test -race -tags=integration ./test/integration/ -run TestTheStaffDeviceListNeverNames -v
```

Expected: FAIL — 404, the route does not exist.

- [ ] **Step 3: Implement the handlers**

`internal/apiserver/staff_read.go` maps existing service calls to the narrowed shapes. The device projection is the part that matters:

```go
// staffDeviceView is deliberately not the admin DeviceSummary. It names the
// device and when it is expected back, and carries no borrower at all — the
// type system is doing the privacy work here, so a future field on the admin
// shape cannot leak through this endpoint by accident.
func staffDeviceView(d catalogapi.DeviceSummary, openLoan *lendingapi.Loan) gen.StaffDevice {
	view := gen.StaffDevice{
		Id: d.ID, AssetTag: d.AssetTag, Name: d.Name,
		Availability: gen.StaffDeviceAvailabilityAvailable,
	}
	if d.Status != catalogapi.DeviceStatusActive {
		view.Availability = gen.StaffDeviceAvailabilityUnavailable
		return view
	}
	if openLoan != nil {
		view.Availability = gen.StaffDeviceAvailabilityInUse
		view.ExpectedBackAt = openLoan.DueAt
	}
	return view
}
```

`GET /v1/staff/me/credential` calls `s.credentials.ListBySubject` for the signed-in user, finds the active QR, and calls `s.credentials.Reveal` with actor `staff:<userID>` — so a staff member reading their own card is audited the same way an administrator is. If there is no active credential, answer 404 with a `Problem` of type `no-credential`, which the app renders as "ask an administrator for a card".

- [ ] **Step 4: Run the tests**

```bash
task generate && cd hdms-backend && go test -race -tags=integration ./test/integration/ -run TestTheStaff -v && golangci-lint run ./...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add hdms-backend hdms-frontend/packages/api-client/src
git commit -m "feat(phase-7d): staff-facing device, loan and credential reads"
```

---

### Task 9: Administrator changes — password reset and the credential panel

**Files:**
- Modify: `hdms-backend/api/openapi.yaml`, `internal/apiserver/users.go`, `internal/platform/auth/roles.go`
- Modify: `hdms-frontend/apps/admin/src/components/credentials-panel.tsx`
- Modify: `hdms-frontend/apps/admin/src/i18n/en.ts`, `ja.ts`
- Test: `hdms-frontend/apps/admin/src/__tests__/credentials-panel.test.tsx`, `hdms-backend/test/integration/staff_password_reset_test.go`

**Interfaces:**
- Consumes: `staffAuth.EnsureAccount`, `SetPassword`, `RevokeAllSessions`; `revealCredential` from Task 7.
- Produces: `POST /v1/users/{id}/staff-password-reset` returning `{temporaryPassword: string}`, classified `admin`.

- [ ] **Step 1: Write the failing backend test**

```go
//go:build integration

package integration

import (
	"context"
	"testing"
)

func TestAPasswordResetEndsEveryExistingStaffSession(t *testing.T) {
	env := newHTTPTestEnv(t)
	ctx := context.Background()

	user := env.SeedUser(t, "E-RESET-1", "Reset Person", "reset@hospital.example")
	account := env.SeedStaffAccountWithPassword(t, user.ID, "old-password-here")
	token, _, err := env.StaffAuth.StartSession(ctx, account.ID)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	if _, err := env.Server.ResetStaffPasswordForTest(ctx, user.ID, "admin:test"); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if _, err := env.StaffAuth.ValidateSession(ctx, token); err == nil {
		t.Fatal("a session survived a password reset; every session must end")
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

```bash
cd hdms-backend && go test -race -tags=integration ./test/integration/ -run TestAPasswordResetEndsEvery -v
```

Expected: FAIL to compile.

`ResetStaffPasswordForTest` and `SeedStaffAccountWithPassword` are thin
helpers added alongside the handler and the test env respectively, in the same
shape as `ResolveOrProvisionForTest` from Task 6 — the handler logic itself
lives in `apiserver`, and the wrapper only makes it callable without an HTTP
round trip.

- [ ] **Step 3: Implement the reset**

Generate a 16-character random temporary password, `SetPassword(..., mustChange: true)`, `RevokeAllSessions`, audit `staff.password_reset`, and return the plaintext once in the response — it is shown to the administrator to hand over and never stored anywhere else.

- [ ] **Step 4: Write the failing admin-console test**

In `credentials-panel.test.tsx`:

```tsx
it("offers view and print on a user card and no reissue-to-reprint", async () => {
  renderPanel({ subjectType: "user", credentials: [activeUserCard] });

  expect(await screen.findByRole("button", { name: /view qr/i })).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: /^reissue$/i })).not.toBeInTheDocument();
  // The lost-card path survives: it is the only way to invalidate a card
  // someone can no longer find (ADR-0016).
  expect(screen.getByRole("button", { name: /report lost/i })).toBeInTheDocument();
});

it("keeps reissue and reprint on a device credential", async () => {
  renderPanel({ subjectType: "device", credentials: [activeDeviceCredential] });

  expect(await screen.findByRole("button", { name: /reissue/i })).toBeInTheDocument();
  expect(screen.getByRole("button", { name: /reprint/i })).toBeInTheDocument();
});
```

Match `renderPanel` and the fixture names to whatever the existing test file already defines.

- [ ] **Step 5: Change the panel**

For `subjectType === "user"`: replace the reissue button with a View QR button calling `revealCredential`, feeding the returned token into the existing `TokenRevealDialog` (which already carries print and export). Keep the lost-card action. For `subjectType === "device"`: unchanged. Add the new strings to `en.ts` and `ja.ts` in the same commit.

- [ ] **Step 6: Run the tests**

```bash
cd hdms-frontend && pnpm --filter admin test && pnpm -r lint
cd ../hdms-backend && go test -race -tags=integration ./test/... 
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add hdms-backend hdms-frontend
git commit -m "feat(phase-7d): admin staff password reset and the user-card reveal action"
```

---

### Task 10: Scaffold `apps/staff`

**Files:**
- Create: `hdms-frontend/apps/staff/package.json`, `index.html`, `vite.config.ts`, `vitest.config.ts`, `tsconfig.json`, `tsconfig.app.json`, `tsconfig.node.json`, `.oxlintrc.json`, `.env.example`, `README.md`
- Create: `hdms-frontend/apps/staff/src/main.tsx`, `App.tsx`, `index.css`, `router.tsx`, `routes/root.tsx`, `routes/index.tsx`
- Create: `hdms-frontend/apps/staff/src/i18n/index.ts`, `en.ts`, `ja.ts`
- Create: `hdms-frontend/apps/staff/src/test/setup.ts`
- Create: `hdms-frontend/apps/staff/public/pwa-192x192.png`, `pwa-512x512.png`, `apple-touch-icon.png`
- Modify: `Taskfile.yml` (`frontend:dev` and `frontend:dev:remote` filters)
- Test: `hdms-frontend/apps/staff/src/App.test.tsx`

**Interfaces:**
- Consumes: `@hdms/api-client`, `@hdms/i18n`, `@hdms/ui`.
- Produces: a dev server on port 5175, `useT()` from `@/i18n`, and the route tree the later tasks add screens to.

- [ ] **Step 1: Copy the kiosk app's configuration as the starting point**

```bash
cd hdms-frontend/apps && mkdir -p staff/src staff/public
cp kiosk/tsconfig*.json kiosk/.oxlintrc.json staff/
cp kiosk/vitest.config.ts staff/
```

Then write `staff/package.json`, mirroring kiosk's but named `staff`, dropping `@hdms/domain`, `@hdms/scan` and `xstate` — the staff app neither scans nor drives the session machine:

```json
{
  "name": "staff",
  "private": true,
  "version": "0.0.0",
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "tsc -b && vite build",
    "lint": "oxlint",
    "preview": "vite preview",
    "test": "vitest run"
  },
  "dependencies": {
    "@hdms/api-client": "workspace:*",
    "@hdms/i18n": "workspace:*",
    "@hdms/ui": "workspace:*",
    "@tanstack/react-query": "^5.101.4",
    "@tanstack/react-router": "^1.170.30",
    "bwip-js": "^4.9.0",
    "class-variance-authority": "^0.7.1",
    "lucide-react": "^1.32.0",
    "radix-ui": "^1.6.7",
    "react": "^19.2.8",
    "react-dom": "^19.2.8",
    "react-hook-form": "^7.66.0",
    "zod": "^4.4.3"
  },
  "devDependencies": {
    "@tailwindcss/vite": "^4.3.3",
    "@testing-library/jest-dom": "^7.0.1",
    "@testing-library/react": "^16.3.2",
    "@testing-library/user-event": "^14.6.5",
    "@types/node": "^24.13.3",
    "@types/react": "^19.2.17",
    "@types/react-dom": "^19.2.3",
    "@vitejs/plugin-react": "^6.0.4",
    "jsdom": "^30.0.1",
    "oxlint": "^1.75.0",
    "tailwindcss": "^4.3.3",
    "typescript": "~6.0.2",
    "vite": "^8.2.0",
    "vite-plugin-pwa": "^1.3.0",
    "vitest": "^4.1.11",
    "vitest-axe": "^0.1.0"
  }
}
```

Pin every version to whatever `apps/admin/package.json` and `apps/kiosk/package.json` already use — check `bwip-js` and `react-hook-form` in admin's manifest and copy those exact ranges rather than the ones above if they differ.

- [ ] **Step 2: Write the Vite config**

`staff/vite.config.ts` is kiosk's with three changes: port `5175`, a phone-shaped manifest, and no camera-related comment.

```ts
export const pwaOptions = {
  registerType: "autoUpdate" as const,
  workbox: {
    globPatterns: ["**/*.{js,css,html,ico,png,svg}"],
    navigateFallback: "index.html",
    // The staff app has no offline story in Phase 7: every data screen says
    // it needs a connection rather than serving a stale answer about who
    // has what.
    runtimeCaching: [{ urlPattern: /\/v1\/.*/, handler: "NetworkOnly" as const }],
  },
  manifest: {
    name: "HDMS Staff",
    short_name: "HDMS",
    description: "Hito Device Management System — your card, your loans, the device catalogue",
    display: "standalone" as const,
    orientation: "portrait" as const,
    start_url: "/",
    background_color: "#ffffff",
    theme_color: "#0a0a0a",
    icons: [
      { src: "pwa-192x192.png", sizes: "192x192", type: "image/png" },
      { src: "pwa-512x512.png", sizes: "512x512", type: "image/png" },
      { src: "apple-touch-icon.png", sizes: "180x180", type: "image/png", purpose: "maskable" },
    ],
  },
};
```

Keep the same HTTPS-with-mkcert block and the `/v1` proxy the kiosk uses; iOS refuses to install a PWA served over plain HTTP.

- [ ] **Step 3: Write the failing smoke test**

`staff/src/App.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import App from "./App";

describe("staff app shell", () => {
  it("renders the app title from the catalogue, not a literal", async () => {
    render(<App />);
    expect(await screen.findByText("HDMS")).toBeInTheDocument();
  });
});
```

- [ ] **Step 4: Run it and watch it fail**

```bash
cd hdms-frontend && pnpm install && pnpm --filter staff test
```

Expected: FAIL — `Cannot find module './App'`.

- [ ] **Step 5: Write the shell**

`src/i18n/en.ts` and `ja.ts` start with the app name, a loading string and an offline string; `src/i18n/index.ts` mirrors kiosk's exactly (the `Catalogues<T>` + `useTranslator` shape shown in `apps/kiosk/src/i18n/index.ts`). `App.tsx` mounts the `I18nProvider`, the `QueryClientProvider` and the `RouterProvider`. `main.tsx` is three lines, copied from kiosk.

- [ ] **Step 6: Add the app to the dev tasks**

In `Taskfile.yml`, both frontend dev tasks become:

```yaml
      - pnpm --parallel --filter kiosk --filter admin --filter staff dev
```

- [ ] **Step 7: Run the tests and the build**

```bash
cd hdms-frontend && pnpm --filter staff test && pnpm --filter staff build && pnpm -r lint
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add hdms-frontend/apps/staff hdms-frontend/pnpm-lock.yaml Taskfile.yml
git commit -m "feat(phase-7e): scaffold the staff PWA"
```

---

### Task 11: The login screen

**Files:**
- Create: `hdms-frontend/apps/staff/src/routes/login.tsx`
- Create: `hdms-frontend/apps/staff/src/lib/auth.ts`
- Create: `hdms-frontend/apps/staff/src/routes/authenticated.tsx`
- Modify: `src/router.tsx`, `src/i18n/en.ts`, `src/i18n/ja.ts`
- Test: `hdms-frontend/apps/staff/src/__tests__/login.test.tsx`

**Interfaces:**
- Consumes: `staffPasswordLogin`, `getStaffMe` from `@hdms/api-client`.
- Produces:

```ts
export async function loginWithPassword(input: {employeeNo: string; password: string}): Promise<StaffMe>
export async function fetchMe(): Promise<StaffMe | null>
export function microsoftSignInUrl(): string   // "/v1/staff/auth/microsoft/start"
export async function logout(): Promise<void>
```

- [ ] **Step 1: Write the failing test**

```tsx
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { LoginPage } from "@/routes/login";

describe("staff login", () => {
  it("offers Microsoft sign-in and an employee-number form on the same screen", () => {
    render(<LoginPage />);
    expect(screen.getByRole("link", { name: /continue with microsoft/i })).toHaveAttribute(
      "href",
      "/v1/staff/auth/microsoft/start",
    );
    expect(screen.getByLabelText(/employee number/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/password/i)).toBeInTheDocument();
  });

  it("shows one message for a wrong employee number and a wrong password", async () => {
    const submit = vi.fn().mockRejectedValue({ status: 401 });
    render(<LoginPage onSubmit={submit} />);

    await userEvent.type(screen.getByLabelText(/employee number/i), "E-1");
    await userEvent.type(screen.getByLabelText(/password/i), "wrong-password");
    await userEvent.click(screen.getByRole("button", { name: /sign in/i }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      /employee number or password is incorrect/i,
    );
  });
});
```

- [ ] **Step 2: Run it and watch it fail**

```bash
cd hdms-frontend && pnpm --filter staff test
```

Expected: FAIL — `Cannot find module '@/routes/login'`.

- [ ] **Step 3: Write the screen**

Follow `apps/admin/src/routes/login.tsx` closely: `react-hook-form` with a zod schema whose messages are catalogue keys, `useLocalizedResolver`, a `useMutation` for the submit, and `role="alert"` on the error. The Microsoft button is a plain `<a href="/v1/staff/auth/microsoft/start">` — a full-page navigation, not `fetch`, because the browser must follow the redirect to Entra.

Read `?error=` from the URL on mount and render the matching catalogue message, since the callback redirects failures back here rather than showing a raw problem document. Cover at least `tenant_not_allowed`, `domain_not_allowed`, `state_unknown` and a generic fallback.

- [ ] **Step 4: Run the tests**

```bash
cd hdms-frontend && pnpm --filter staff test && pnpm --filter staff lint
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add hdms-frontend/apps/staff
git commit -m "feat(phase-7e): staff login with Microsoft and employee number"
```

---

### Task 12: The first-run completion screen and the session guard

**Files:**
- Create: `hdms-frontend/apps/staff/src/routes/complete-profile.tsx`
- Modify: `src/routes/authenticated.tsx` (the guard), `src/router.tsx`, both catalogues
- Test: `hdms-frontend/apps/staff/src/__tests__/complete-profile.test.tsx`

**Interfaces:**
- Consumes: `getStaffMe`, `completeStaffProfile`.
- Produces: a guard that sends `profileComplete === false` to `/complete-profile` and `mustChangePassword === true` to `/change-password`, from anywhere in the app.

- [ ] **Step 1: Write the failing test**

```tsx
it("sends a signed-in user with an incomplete profile to the completion screen", async () => {
  const me = { userId: "u1", employeeNo: "MS-AB12CD34", fullName: "New Person",
               status: "active", profileComplete: false, mustChangePassword: false,
               hasPassword: false, signInMethods: ["microsoft"] };
  renderApp({ me, initialPath: "/devices" });

  expect(await screen.findByRole("heading", { name: /one more thing/i })).toBeInTheDocument();
  expect(screen.getByLabelText(/employee number/i)).toBeInTheDocument();
});

it("explains why the employee number is needed rather than just demanding it", async () => {
  const me = { userId: "u1", employeeNo: "MS-AB12CD34", fullName: "New Person",
               status: "active", profileComplete: false, mustChangePassword: false,
               hasPassword: false, signInMethods: ["microsoft"] };
  renderApp({ me, initialPath: "/" });

  expect(await screen.findByText(/your qr code is created once we have it/i)).toBeInTheDocument();
});
```

- [ ] **Step 2: Run it and watch it fail**

```bash
cd hdms-frontend && pnpm --filter staff test
```

Expected: FAIL — no such route.

- [ ] **Step 3: Implement the guard and the screen**

The guard lives in the `authenticated` route's `beforeLoad`, reading the `me` query from the query client. The screen is one field and one button, with the explanation above it, and on success it invalidates `me` and navigates home — where the QR now exists.

- [ ] **Step 4: Run the tests**

```bash
cd hdms-frontend && pnpm --filter staff test && pnpm --filter staff lint
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add hdms-frontend/apps/staff
git commit -m "feat(phase-7e): first-run profile completion and the session guard"
```

---

### Task 13: Home — my QR and my loans

**Files:**
- Create: `hdms-frontend/apps/staff/src/routes/home.tsx`
- Create: `hdms-frontend/apps/staff/src/components/my-qr.tsx`
- Create: `hdms-frontend/apps/staff/src/lib/screen.ts`
- Modify: `src/router.tsx`, both catalogues
- Test: `hdms-frontend/apps/staff/src/__tests__/my-qr.test.tsx`

**Interfaces:**
- Consumes: `getStaffMeCredential`, `getStaffMeLoans`, `bwip-js/browser`.
- Produces:

```ts
export function MyQr(props: {token: string}): JSX.Element
export async function withMaxBrightness<T>(run: () => Promise<T>): Promise<T>
```

- [ ] **Step 1: Write the failing test**

```tsx
it("renders the QR as an image with an accessible name, not a bare canvas", async () => {
  render(<MyQr token="HDMS-TEST-TOKEN" />);
  expect(await screen.findByRole("img", { name: /your qr code/i })).toBeInTheDocument();
});

it("tells the user what to do with it", async () => {
  render(<MyQr token="HDMS-TEST-TOKEN" />);
  expect(await screen.findByText(/hold this up to the kiosk scanner/i)).toBeInTheDocument();
});

it("asks for a wake lock while the code is on screen and releases it on unmount", async () => {
  const release = vi.fn().mockResolvedValue(undefined);
  const request = vi.fn().mockResolvedValue({ release });
  vi.stubGlobal("navigator", { ...navigator, wakeLock: { request } });

  const { unmount } = render(<MyQr token="HDMS-TEST-TOKEN" />);
  await waitFor(() => expect(request).toHaveBeenCalledWith("screen"));
  unmount();
  await waitFor(() => expect(release).toHaveBeenCalled());
});

it("still renders when wake lock is unavailable", async () => {
  vi.stubGlobal("navigator", { ...navigator, wakeLock: undefined });
  render(<MyQr token="HDMS-TEST-TOKEN" />);
  expect(await screen.findByRole("img", { name: /your qr code/i })).toBeInTheDocument();
});
```

The last case is the one that matters on iOS, where `navigator.wakeLock` is absent in some contexts: an unavailable wake lock must never stop the code from being shown.

- [ ] **Step 2: Run it and watch it fail**

```bash
cd hdms-frontend && pnpm --filter staff test
```

Expected: FAIL — `Cannot find module '@/components/my-qr'`.

- [ ] **Step 3: Implement**

Render through `bwip-js/browser` into a canvas, then `toDataURL` into an `<img alt={t("myQr.alt")}>` so the code carries an accessible name and can be long-pressed and saved on iOS. Request the wake lock in an effect wrapped in `try`/`catch`, release it in the cleanup, and re-request on `visibilitychange`, because iOS drops the lock when the app backgrounds.

The loans list below it uses the same query/table conventions as the admin app's loan list, narrowed to device name, borrowed date and due date.

- [ ] **Step 4: Run the tests**

```bash
cd hdms-frontend && pnpm --filter staff test && pnpm --filter staff lint
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add hdms-frontend/apps/staff
git commit -m "feat(phase-7e): the home screen with the staff QR and current loans"
```

---

### Task 14: Devices — list and detail

**Files:**
- Create: `hdms-frontend/apps/staff/src/routes/devices.tsx`, `src/routes/devices.$deviceId.tsx`
- Modify: `src/router.tsx`, both catalogues
- Test: `hdms-frontend/apps/staff/src/__tests__/devices.test.tsx`

**Interfaces:**
- Consumes: `getStaffDevices`, `getStaffDevice`.
- Produces: the list and detail screens the Phase 8 reserve button will hang from.

- [ ] **Step 1: Write the failing test**

```tsx
it("shows availability and the expected return time, and never a borrower", async () => {
  renderDevices({
    items: [
      { id: "d1", assetTag: "AT-1", name: "Projector", availability: "in_use",
        expectedBackAt: "2026-09-15T15:30:00Z" },
      { id: "d2", assetTag: "AT-2", name: "Laptop", availability: "available" },
    ],
  });

  expect(await screen.findByText("Projector")).toBeInTheDocument();
  expect(screen.getByText(/in use/i)).toBeInTheDocument();
  expect(screen.getByText(/15:30/)).toBeInTheDocument();
  expect(screen.getByText(/available/i)).toBeInTheDocument();
});

it("says the list needs a connection rather than showing a stale one", async () => {
  renderDevices({ error: new TypeError("Failed to fetch") });
  expect(await screen.findByText(/you need a connection to see devices/i)).toBeInTheDocument();
});
```

- [ ] **Step 2: Run it and watch it fail**

```bash
cd hdms-frontend && pnpm --filter staff test
```

Expected: FAIL — no such route.

- [ ] **Step 3: Implement**

A search field over asset tag and name, a status pill, and the expected-back time formatted through `@hdms/i18n`'s formatter so it is correct in both locales. Detail adds model and category. Long device names and long asset tags must wrap rather than overflow — follow the resilience rules in `docs/phases/phase-3/3.9-visual-design.md`.

- [ ] **Step 4: Run the tests**

```bash
cd hdms-frontend && pnpm --filter staff test && pnpm --filter staff lint
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add hdms-frontend/apps/staff
git commit -m "feat(phase-7e): the staff device list and detail screens"
```

---

### Task 15: Settings — password, language, sign out

**Files:**
- Create: `hdms-frontend/apps/staff/src/routes/settings.tsx`, `src/routes/change-password.tsx`
- Modify: `src/router.tsx`, both catalogues
- Test: `hdms-frontend/apps/staff/src/__tests__/change-password.test.tsx`

**Interfaces:**
- Consumes: `changeStaffPassword`, `staffLogout`, the locale switcher from `@hdms/i18n`.
- Produces: the forced-change screen the guard from Task 12 redirects to.

- [ ] **Step 1: Write the failing test**

```tsx
it("requires the current password and a new one of at least 12 characters", async () => {
  renderChangePassword();

  await userEvent.type(screen.getByLabelText(/current password/i), "temporary");
  await userEvent.type(screen.getByLabelText(/new password/i), "short");
  await userEvent.click(screen.getByRole("button", { name: /save/i }));

  expect(await screen.findByText(/at least 12 characters/i)).toBeInTheDocument();
});

it("explains, on a forced change, why it is being asked", async () => {
  renderChangePassword({ forced: true });
  expect(
    await screen.findByText(/this password was set for you — choose your own/i),
  ).toBeInTheDocument();
});

it("hides the password section entirely for a Microsoft-only account", async () => {
  renderSettings({ me: { hasPassword: false, signInMethods: ["microsoft"] } });
  expect(screen.queryByRole("link", { name: /change password/i })).not.toBeInTheDocument();
});
```

- [ ] **Step 2: Run it and watch it fail**

```bash
cd hdms-frontend && pnpm --filter staff test
```

Expected: FAIL — no such route.

- [ ] **Step 3: Implement**

Three sections: account (name, employee number, department, how you sign in), password (only when `hasPassword`), and language plus sign out. Sign out calls `staffLogout` and clears the query cache.

- [ ] **Step 4: Run the tests**

```bash
cd hdms-frontend && pnpm --filter staff test && pnpm --filter staff lint && pnpm --filter staff build
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add hdms-frontend/apps/staff
git commit -m "feat(phase-7e): staff settings, password change and sign out"
```

---

### Task 16: End-to-end proof, decision records and documentation

**Files:**
- Create: `hdms-frontend/e2e/staff-login.spec.ts`
- Create: `docs/adr/0009-staff-authentication-realm.md`, `docs/adr/0012-relaxing-administrator-only-registration.md`, `docs/adr/0016-user-credential-tokens-stored-reversibly.md`
- Create: `docs/phases/phase-7-staff-identity.md`
- Create: `docs/runbooks/credential-encryption-key.md`
- Modify: `docs/README.md`, `docs/05-credentials-and-labeling.md`, `docs/06-api-contract.md`, `docs/09-security-privacy-ops.md`, `docs/phases/phase-6/6.3-directory-integration.md`

**Interfaces:**
- Consumes: everything above.
- Produces: the documentation set telling the truth about the system as it now stands.

- [ ] **Step 1: Write the end-to-end test**

`hdms-frontend/e2e/staff-login.spec.ts`, following whatever fixtures the existing Playwright specs use:

```ts
test("a staff member signs in with an employee number, changes a temporary password, and sees their QR", async ({ page }) => {
  await page.goto("/");
  await page.getByLabel(/employee number/i).fill("E-E2E-1");
  await page.getByLabel(/password/i).fill(process.env.E2E_TEMP_PASSWORD!);
  await page.getByRole("button", { name: /sign in/i }).click();

  // The temporary password forces a change before anything else is reachable.
  await expect(page.getByRole("heading", { name: /choose your own/i })).toBeVisible();
  await page.getByLabel(/new password/i).fill("a-much-longer-password");
  await page.getByRole("button", { name: /save/i }).click();

  await expect(page.getByRole("img", { name: /your qr code/i })).toBeVisible();
});

test("the same journey reads correctly in Japanese", async ({ page }) => {
  await page.goto("/?lang=ja");
  await expect(page.getByRole("button", { name: "サインイン" })).toBeVisible();
});
```

Run: `cd hdms-frontend && pnpm exec playwright test e2e/staff-login.spec.ts`

- [ ] **Step 2: Write the three decision records**

Follow the shape of `docs/adr/0008-temporal-custody-constraint.md`: context, decision, consequences, and what would make us revisit it.

- **ADR-0009** — a staff realm rather than a role on `admin_accounts` or password columns on `users`; the browser holds a session, never an Entra token.
- **ADR-0012** — Entra tenant membership replaces the administrator as the authorising decision for registration. Say plainly what is given up: the hospital can no longer assume a person in the users table was vetted by an administrator, and an incomplete self-signup holds a placeholder employee number until its owner completes it. Say what did not change: the kiosk still never creates a user.
- **ADR-0016** — user tokens stored reversibly, reversing `docs/05`'s choice (b). Record the cost — the deployment key now protects every credential in the system — and the compensating controls: admin-only reveal, an audit row per reveal, and the retained lost-card revoke-and-issue path.

- [ ] **Step 3: Correct the existing documents**

`docs/05` gains a note at its design-note box saying choice (b) was reversed in Phase 7 and pointing at ADR-0016; the box itself stays, because the reasoning that produced it is still the reasoning someone will need when they ask why the key matters. `docs/06` gains the staff paths. `docs/09` gains the staff principal in its authentication section and a line in its PII section: no staff-facing payload names another person. `docs/README.md` gains Phase 7 in the phase table. `docs/phases/phase-6/6.3-directory-integration.md` gains a note that 6.3a now inherits a working OIDC client from Phase 7 and that ADR-0012 has been written.

- [ ] **Step 4: Write the key runbook**

`docs/runbooks/credential-encryption-key.md`: where `HDMS_CREDENTIAL_ENC_KEY` lives, who can read it, how to rotate it (re-encrypt every `token_enc` under the new key in one transaction, with the old key held until the job completes), and what breaks if it is lost — every QR becomes unrecoverable and every card must be reissued, while scanning keeps working because that path uses the HMAC.

- [ ] **Step 5: Write the phase document**

`docs/phases/phase-7-staff-identity.md`, in the shape of the other phase documents: goal, duration, dependencies, sub-phases matching this plan's task groups, exit criteria, and a risks table. Exit criteria:

- Both sign-in paths work on an iPhone with the app installed to the home screen.
- A self-signed-up person reaches a working QR without an administrator touching anything.
- An administrator can view and print any active user card, and every reveal appears in the audit log.
- A lost card can still be revoked and replaced.
- An admin cookie cannot reach `/v1/staff/*` and a staff cookie cannot reach `/v1/*` admin routes, both proven by tests.
- The kiosk borrow-and-return path is unchanged, proven by the existing suite.

- [ ] **Step 6: Run the full gate**

```bash
task lint && task test && task test:integration && task build && cd hdms-frontend && pnpm exec playwright test
```

Expected: all green. This is the gate that matters; do not report the phase complete on anything less.

- [ ] **Step 7: Commit**

```bash
git add docs hdms-frontend/e2e
git commit -m "docs(phase-7): decision records, runbook and the phase-7 plan documentation"
```

---

## Notes for the executor

- **Tasks 1-9 are backend and must be done in order**; each depends on the one before. Tasks 10-15 are frontend and depend only on Task 6's contract being generated — they can be worked in parallel with 7-9 by a second worker if there is one.
- **Do not skip the "watch it fail" steps.** Several of these tests pass trivially against a mistaken implementation; seeing the intended failure first is what proves the test is wired to the thing it claims to test.
- **When a generated file and a hand-written one disagree, the contract wins.** Edit `api/openapi.yaml`, re-run `task generate`, and fix the handlers — never the generated code.
- **If a step's code does not compile against the real tree**, prefer the tree: the surrounding conventions were read when this plan was written, but signatures drift. Match what is there and say so in the commit body.
