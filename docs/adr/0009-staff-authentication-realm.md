# ADR-0009 — Dedicated staff authentication realm

**Status:** Accepted · **Date:** 2026-09-15 · **Related to** [ADR-0001](0001-modular-monolith-go.md)

## Context

In Phase 7, hospital staff are given self-service access to view their own borrower QR code, inspect their active equipment loans, and browse available devices through a mobile PWA. Staff authenticate either via enterprise single sign-on (Microsoft Entra ID / OIDC) or with their employee number and password.

Before Phase 7, the system recognized two authenticated principals:
1. **Administrators**, who authenticate via `admin_accounts` using email, password, and TOTP, managed by `internal/platform/auth`.
2. **Kiosks**, which authenticate using long-lived bearer tokens tied to physical counter terminals.

When introducing staff credentials and sessions, three design alternatives were considered:
1. **Add a role or flag to `admin_accounts`:** Treat staff members as low-privilege administrators.
2. **Add password hashes and session columns directly to `users`:** Store authentication state directly on the domain user entity.
3. **Establish a dedicated staff authentication realm:** Create separate data models (`staff_accounts`, `staff_identities`, `staff_sessions`) and a separate platform package (`internal/platform/staffauth`).

Furthermore, for single sign-on with Microsoft Entra ID, a choice was required regarding token handling: whether the mobile browser should receive and manage Entra ID access/refresh tokens, or whether Entra tokens should terminate at the backend API server.

## Decision

We establish a **dedicated staff authentication realm** isolated from the administrative realm, and we terminate all external identity tokens at the server:

1. **Dedicated tables:** We introduce `staff_accounts`, `staff_identities`, and `staff_sessions`. There is intentionally no shared session table and no role hierarchy linking staff and administrators.
2. **Dedicated platform package:** `internal/platform/staffauth` manages staff passwords, lockout counters, external identity links, and session tokens. It operates strictly on UUID user IDs and has no knowledge of domain entities (users, devices, or loans). Cross-boundary orchestration (e.g., linking an Entra claim to a user record or minting a new QR) is handled one layer up in `apiserver`.
3. **Live user status evaluation:** `staff_accounts` does not duplicate a user status column. The server reads `users.status` live on request processing, ensuring account suspension or archival takes effect immediately without synchronization lag.
4. **Server-side session translation:** The browser never receives an Entra ID ID token or access token. The OIDC authorization code flow completes on the backend, which issues a standard opaque HDMS staff session cookie (`hdms_staff_session`, `HttpOnly`, `Secure`, `SameSite=Lax`) and CSRF token.
5. **Strict route separation:** Staff endpoints are mounted exclusively under `/v1/staff/*`. Admin session cookies are rejected on `/v1/staff/*`, and staff session cookies are rejected on administrative routes (`/v1/*`).

## Consequences

**Good**
- **Zero privilege escalation risk between realms:** A staff session cookie can never satisfy administrative middleware. Admin queries do not need to add defensive clauses (`WHERE role != 'staff'`).
- **Clean domain boundaries:** The `users` table remains a pure representation of hospital personnel and custody subjects, free of web authentication tokens, temporary password flags, or failed login counters.
- **Minimal client footprint:** Mobile browsers hold only an opaque session cookie. No token refresh loops, token storage vulnerabilities, or OIDC client logic exist in the staff PWA bundle.
- **Auditable credential boundaries:** External provider links (`staff_identities`) are distinct from password-backed credentials, allowing passwordless accounts to exist naturally alongside password-authenticated staff.

**Bad**
- **Parallel infrastructure:** Session management, sliding expiration, and lockout logic exist in both `auth` and `staffauth`.
- **Two cookies:** An administrator testing the staff PWA in the same browser maintains two independent session cookies (`hdms_session` and `hdms_staff_session`).

## Alternatives

- **Unified account model in `admin_accounts`:** Rejected. Administrators require email addresses and mandatory TOTP, whereas hospital staff log in with employee numbers and may authenticate exclusively via Microsoft SSO without local passwords or TOTP. Conflating the two would compromise admin security invariants.
- **Credentials embedded in `users`:** Rejected. Violates the module decoupling of ADR-0001 and creates schema churn on a core entity whenever authentication mechanisms change.
- **Client-side OIDC with Entra JWTs passed to the API:** Rejected. Exposes raw external tokens to client-side storage, requires complex token refresh handling in the PWA, and forces the backend to validate third-party signatures on every request rather than evaluating our own lightweight session store.

## What would make us revisit this

If the hospital migrates to a fully unified identity fabric where all administrative access and operational access are governed by fine-grained claims within a single IdP and managed by a centralized policy engine (e.g., Open Policy Agent or SpiceDB).
