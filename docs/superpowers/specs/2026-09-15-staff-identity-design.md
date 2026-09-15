# Staff identity, self-signup and the staff PWA — design

**Date:** 2026-09-15 · **Status:** approved, ready for an implementation plan
**Scope:** Phase 7 · **Follows:** Phase 6.9 (i18n Release Two)

## Why this exists

Until now the system has had two kinds of principal: administrators, who sign in
to the console with a password and TOTP, and kiosks, which hold a paired device
token. Staff have no login at all — they are records that other people act upon.

The hospital now wants staff to hold the system in their own hand: a PWA on an
iPhone that signs in with their Microsoft account, shows them their own QR code
so they can be scanned at the kiosk without carrying a card, tells them what they
have out, and — from Phase 8 — lets them reserve a device. Registering a staff
member should no longer require an administrator to type them in.

That is a third principal and a second authentication authority, so it gets its
own phase and its own architecture decision records.

## What was decided

| Question | Decision |
|---|---|
| Delivery shape | Three phases. **Phase 7: staff identity, self-signup, credential reveal, PWA shell.** Phase 8: reservations. Phase 9: notifications |
| What replaces administrator-only registration | Entra tenant membership. Anyone who can sign in with a Microsoft account in the hospital tenant is provisioned automatically |
| Where the staff login lives | A separate staff realm — its own accounts, sessions, cookie and middleware — built on `platform/auth`'s existing primitives |
| How Microsoft sign-in is wired | Backend-mediated authorization code + PKCE. No Entra token ever reaches the browser |
| Where the PWA lives | A third app, `hdms-frontend/apps/staff` |
| What a staff member types instead of a username | Their employee number |
| Can one person hold both login paths | Yes. One `users` row, one QR, two doors |
| Who may reveal a user's plaintext QR | The `admin` role only, every reveal audited |
| What other staff see about a device in use | Availability and expected return time. Never the borrower's name |
| Existing cards | There are none. This is a new deployment; test data is cleared before rollout |

## Non-goals

- Reservations, expected-return capture and the gap settings — Phase 8.
- Email, SMS or push delivery — Phase 9. Phase 7 sends nothing.
- OIDC for administrators (Phase 6.3a). Phase 7 builds the OIDC client that
  6.3a will reuse, but does not change how administrators sign in.
- Offline behaviour in the staff app beyond app-shell caching. The kiosk's
  offline design is deliberately not extended to personal phones.
- Retiring the physical QR card. The phone screen becomes an additional way to
  present the same credential, not a replacement for it.

## Architecture

### The staff realm

`platform/auth` already owns password hashing, failed-attempt lockout,
server-side sessions in Postgres, session cookies and the CSRF double-submit. The
staff realm reuses all of it and shares none of its rows: separate tables,
separate cookie names, separate middleware scope.

The alternative — a `staff` role on `admin_accounts` — was rejected. Every
existing administrator query and role check would have to start excluding staff,
and a single missed check would expose the console. Putting password columns
directly on `users` was also rejected: a `users` row may be paper-only, imported
or archived, and an authentication principal is a different thing from an
identity record.

### Microsoft sign-in

The Go server runs the authorization code flow with PKCE against Entra,
validates the ID token against the tenant's JWKS, provisions or links the user,
and issues an ordinary staff session cookie. The browser never holds an access
or refresh token. This keeps one session style across the whole system, avoids
storing bearer tokens on a personal phone, and sidesteps redirect handling inside
an iOS standalone PWA — the fragile part of the MSAL.js alternative.

### The staff app

`apps/staff` sits alongside `apps/kiosk` and `apps/admin` and reuses
`packages/ui`, `packages/i18n` and `packages/api-client`. The kiosk app is
device-paired, shared-screen and offline-queueing; the staff app is
personal-device and per-user. Merging them would muddy both threat models for
the sake of one fewer build target.

## Data model

One migration, `0018_staff_auth.sql`. The `credentials` schema does not change —
`token_enc` already exists and is already nullable; Phase 7 changes only which
subjects get it populated.

```sql
CREATE TABLE staff_accounts (
    id                    uuid PRIMARY KEY,
    user_id               uuid NOT NULL UNIQUE,   -- no FK: crosses the identity
                                                  -- module boundary, as credentials does
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

CREATE TABLE staff_identities (
    id                uuid PRIMARY KEY,
    staff_account_id  uuid NOT NULL REFERENCES staff_accounts(id) ON DELETE CASCADE,
    provider          text NOT NULL,              -- 'microsoft'
    subject           text NOT NULL,              -- the Entra object id (oid)
    tenant_id         text NOT NULL,
    email_at_link     text,
    linked_at         timestamptz NOT NULL DEFAULT now(),
    UNIQUE (provider, subject)
);

CREATE TABLE staff_sessions (              -- mirrors admin_sessions exactly
    id                  uuid PRIMARY KEY,
    staff_account_id    uuid NOT NULL REFERENCES staff_accounts(id) ON DELETE CASCADE,
    session_token_hash  bytea NOT NULL,
    csrf_token          text NOT NULL,
    created_at          timestamptz NOT NULL DEFAULT now(),
    last_seen_at        timestamptz NOT NULL DEFAULT now(),
    expires_at          timestamptz NOT NULL
);

CREATE TABLE oauth_login_states (          -- single-use, 10 minute TTL
    state_hash     bytea PRIMARY KEY,
    verifier_enc   bytea NOT NULL,
    redirect_to    text,
    created_at     timestamptz NOT NULL DEFAULT now(),
    expires_at     timestamptz NOT NULL,
    consumed_at    timestamptz
);

CREATE UNIQUE INDEX users_email_live_uk
    ON users (lower(email)) WHERE email IS NOT NULL AND status <> 'archived';
```

**`staff_accounts` has no status column on purpose.** Login reads `users.status`
live, so suspending or archiving a user takes effect on the next request and
there is no second copy of the truth to reconcile. This is the same reasoning
that keeps a `reserved` device status out of Phase 8.

**The PKCE verifier lives server-side**, not in a cookie, so nothing secret
crosses the redirect. The row is consumed on first use and swept by the existing
scheduled-job pattern.

## Identity linking and provisioning

On a successful Microsoft callback, in this order:

1. A `staff_identities` row matches `(microsoft, oid)` — sign that account in.
2. The token carries an `employeeId`-style claim matching a live user's
   `employee_no` — link a new `staff_identities` row to that user and sign in.
3. The verified email matches a live user's `email`, case-insensitively — link
   and sign in. The new partial unique index makes this unambiguous.
4. Otherwise provision: a new `users` row with `registered_by = 'self:microsoft'`
   and `status = 'active'`, a `staff_accounts` row, and a `staff_identities` row.

**Employee number on provisioning.** When Entra supplies the employee-number
claim it is used directly and `profile_complete` stays true. When it does not,
the row is created with a reserved placeholder, `profile_complete` is false, and
the PWA forces a one-time completion screen that asks for the employee number and
checks it for collisions against live users.

**The QR is minted only once the profile is complete.** An incomplete account can
sign in and browse devices, but has no credential, so it cannot borrow at the
kiosk. This removes the need for a second "is this account real yet" check on the
scan path. The admin users list gains an "incomplete self-signups" filter so the
stragglers are visible.

Email is unverified as far as the system is concerned except that Entra asserted
it; the tenant restriction is what makes it trustworthy. Sign-ins from outside the
configured tenant and domain allowlist are refused before any row is written.

## API surface

Everything is additive, under a new path prefix, per the extension rules in
`docs/phases/phase-6/6.0-intake-and-extension-rules.md`.

| Method | Path | Purpose |
|---|---|---|
| POST | `/api/staff/auth/password` | Employee number + password, issues the staff session |
| GET | `/api/staff/auth/microsoft/start` | Redirects to Entra with PKCE and state |
| GET | `/api/staff/auth/microsoft/callback` | Validates, links or provisions, issues the session |
| POST | `/api/staff/auth/logout` | Ends the session |
| GET | `/api/staff/me` | Profile, status, whether a password is set, whether the profile is complete |
| POST | `/api/staff/me/password` | Self-service change; requires the current password |
| POST | `/api/staff/me/profile` | One-time completion: employee number |
| GET | `/api/staff/me/credential` | The signed-in user's own QR token |
| GET | `/api/staff/devices` | Catalog with availability and expected return |
| GET | `/api/staff/devices/{id}` | Device detail, same disclosure rule |
| GET | `/api/staff/me/loans` | What this user has out, and their history |
| GET | `/api/admin/users/{id}/credentials/{cid}/token` | Reveal for print — `admin` role only, audited |
| POST | `/api/admin/users/{id}/password-reset` | Issue a temporary password, forcing a change |

The device endpoints return `available` or `in_use` with an expected return time
and never a borrower identity. That rule lives in the handler, not the client, and
has its own test.

## Credential reveal policy

`docs/05-credentials-and-labeling.md` chose, deliberately, to store device tokens
reversibly and user tokens as an HMAC only: a device sticker is not a secret, a
staff card is a bearer credential, and a lost card should be reissued rather than
reprinted. The hospital now wants an administrator to be able to see and print any
user's QR at any time. Phase 7 reverses that half of the decision, and
**ADR-0016** records the reversal and its cost honestly.

- `mintToken` stores `token_enc` for `subject_type = 'user'` using the same
  deployment key devices already use.
- The admin user card gains View / Print / Export, reusing `TokenRevealDialog`
  and `StaffCardLabel`. The `admin` role only; `technician` and `viewer` continue
  to see the masked preview.
- Every reveal writes a `credential_events` row with kind `revealed` and the
  actor, exactly as reissue does today.
- The staff member sees their own QR in the PWA. That read is audited too.
- The routine reissue-to-reprint path on a user card is removed, as requested.

**"Report lost → revoke and issue new" stays.** Reprinting a token cannot help a
lost card: the lost card keeps working. Removing every path to a fresh token
would leave a bearer credential permanently unrevocable, so the lost-card flow
that Phase 4 already built remains, under its accurate name, and is the only
thing on a user card that mints a new token.

Devices keep both Reissue and Reprint, unchanged.

Because the token is now recoverable for both subject types, the deployment key
becomes the thing that protects every credential in the system. The runbook entry
for key handling and rotation is part of this phase, not an afterthought.

## Security posture

- Cookies `hdms_staff_session` and `hdms_staff_csrf`: `HttpOnly` where
  applicable, `Secure`, `SameSite=Lax`, 12-hour sliding expiry, the token stored
  hashed, exactly as `admin_sessions`.
- Scope separation is enforced in middleware and proven both ways: an admin
  cookie is rejected on `/api/staff/*`, a staff cookie on `/api/admin/*`.
- Password login reuses the existing lockout counters and is rate-limited per
  employee number and per IP. The response does not distinguish an unknown
  employee number from a wrong password.
- Temporary passwords set by an administrator force a change at next sign-in.
- No TOTP for staff. They hold no administrative capability, and a second factor
  on a borrowing app would push people back to paper.
- New audit events: `staff.login`, `staff.login_failed`, `staff.signup`,
  `staff.identity_linked`, `staff.password_changed`, `staff.password_reset`,
  `credential.revealed`.
- `docs/09-security-privacy-ops.md` gains the staff realm in its principal table
  and its PII section: the staff app shows no other person's name anywhere.

## The staff app

Screens: Login; first-run completion; Home, carrying the QR and the current
loans; Devices, list and detail; Settings, with password change, language and
sign-out.

The login screen is exactly as specified: a "Continue with Microsoft" button and,
below it, an employee-number field with a password. Both paths land in the same
place.

The QR screen raises screen brightness and holds a wake lock where the browser
allows it, because it will be read by a scanner at arm's length. It shows the
same token as the printed card — presenting the phone and presenting the card are
the same act as far as the kiosk is concerned, which means no kiosk change is
needed in this phase.

Installation on an iPhone needs a manifest, an `apple-touch-icon` and
`display: standalone`. Offline, the app shell loads and every data screen says it
needs a connection; nothing is queued. Both locales ship from day one and the
existing lint gate against hardcoded strings applies to the new app.

## Testing

- **Go unit:** the linking ladder, one case per rung, including the ambiguous
  ones — an email that matches an archived user, a claim that matches nobody.
- **Go integration:** `passwordLoginIssuesAStaffSession`,
  `anAdminCookieIsRejectedOnStaffRoutes`, `aStaffCookieIsRejectedOnAdminRoutes`,
  `microsoftCallbackProvisionsAUserExactlyOnce` (run concurrently — provisioning
  races on the same `oid` must produce one user, not two),
  `signInFromAnUntrustedTenantIsRefusedBeforeAnyRowIsWritten`,
  `aSuspendedUserCannotSignIn`, `anIncompleteProfileHasNoCredential`,
  `revealIsRefusedForNonAdminRoles`, `everyRevealWritesACredentialEvent`.
- **Frontend:** vitest and RTL per the existing app conventions, including the
  hostile-props and touch-target suites the kiosk screens already have.
- **End-to-end:** both login paths, the completion screen, a password change and
  a QR display, in both locales.
- **Regression that matters most:** the kiosk borrow and return path is untouched
  by this phase and is re-run to prove it.

## Rollout

The deployment is new and carries no live users, so there is no card migration
and no window of mixed behaviour. Test data is cleared before the tenant is
pointed at the real Entra app registration. The Entra app registration itself —
redirect URI, tenant restriction, and which claims are released — is hospital IT
work that must start before the code does, and is the one external dependency
capable of blocking this phase.

## Architecture decision records to write

- **ADR-0009 — a staff authentication realm separate from administrators.**
  Why a third principal rather than a role, and why the session, not a token,
  is what the browser holds.
- **ADR-0012 — relaxing administrator-only registration.** The number is already
  reserved for this topic by `docs/phases/phase-6/6.3-directory-integration.md`.
  Entra tenant membership replaces the administrator as the authorising decision;
  what that gives up, and what the kiosk still refuses.
- **ADR-0016 — user credential tokens stored reversibly.** The reversal of
  `docs/05`'s choice (b), what it costs, and why the lost-card path survives it.

## Deferred, recorded here so the requirement is not lost

**Phase 8 — reservations.** Lending at the kiosk captures an expected return date
and time rather than deriving a due date from the category. A borrow is refused
when a reservation for that device begins less than the *pre-return gap* after
the stated expected return; the gap defaults to one hour and is administrator-
configurable. A user may reserve a currently borrowed device from the expected
return plus the *handover gap*, defaulting to five minutes and likewise
configurable, and each further reservation chains off the previous one by the
same gap. Changing an expected return time shifts the dependent reservations
accordingly. This sits on top of `docs/phases/phase-6/6.4-reservations.md`, whose
exclusion constraint, walk-up policy and no-show expiry still apply — with the
correction that reservations are now staff-created, which 6.4 could not assume
because no staff login existed.

**Phase 9 — notifications.** A shifted reservation notifies the administrator and
every affected reserver. Until the `notification` module is real, Phase 8 surfaces
the shift in the staff app and the admin console only, and says so on screen.

## Risks

| Risk | Mitigation |
|---|---|
| The Entra app registration is slow or releases no employee-number claim | Start the IT conversation before the code; the completion screen means a missing claim degrades to one extra tap, not a blocker |
| Self-signup creates duplicate users for someone already registered | The three-rung linking ladder before provisioning, plus a concurrency test that a racing callback provisions exactly once |
| A staff session is mistaken for an administrator session | Separate cookie names and middleware scope, with a rejection test in both directions |
| Reversible user tokens widen the blast radius of a database leak | The deployment key stays outside the database, reveal is `admin`-only and audited, and key handling gets a runbook page |
| Removing reissue leaves a lost card working forever | The lost-card revoke-and-issue path is explicitly retained |
| The staff app becomes a directory of who-has-what | No borrower identity in any staff-facing payload, enforced in the handler and tested |
