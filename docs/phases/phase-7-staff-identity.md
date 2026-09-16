# Phase 7 — Staff Identity, Self-Signup and the Staff PWA

**Goal:** Give hospital staff their own login — Microsoft/Entra single sign-on or an employee number and password — a phone PWA that shows their QR code, their loans and the device catalogue, and give administrators the ability to view and print any user's QR at any time.
**Duration:** ~2 weeks · **Depends on:** Phase 1, Phase 4

---

## Overview & Architecture

Phase 7 introduces a third authenticated principal alongside administrators and kiosks: **Hospital Staff**.

The architecture preserves module isolation while delivering self-service onboarding:
- **`internal/platform/staffauth`:** Manages staff accounts, external OIDC identity links, sessions, and password verification. It operates on user IDs only and does not import domain entities.
- **Orchestration in `apiserver`:** The linking ladder (resolving existing users by email or employee number, creating new users, minting initial QR credentials) is orchestrated within `apiserver`, mirroring how `RegisterWithCard` handles identity and credentials.
- **Reversible Token Storage (ADR-0016):** User tokens are stored reversibly (`token_enc`, AES-256-GCM) so the staff PWA can display the QR code on screen and administrators can print cards on demand.
- **Dedicated Staff PWA (`hdms-frontend/apps/staff`):** Built with React 19, TanStack Router, TanStack Query, and Vite PWA, sharing design system components and internationalization catalogues with the admin and kiosk apps.

---

## Sub-phases & Deliverables

### Phase 7a: Domain & Schema Foundations (Tasks 1–5)
- Allow `self:microsoft` as a valid registration provenance in `identity` (ADR-0012).
- Migration `0018_staff_auth.sql`: tables `staff_accounts`, `staff_identities`, `staff_sessions`, `oauth_login_states`, and unique index `users_email_live_uk`.
- Core `staffauth` service with password hashing, account lockout protection (5 failed attempts, 15 min lockout), and sliding session cookies (`hdms_staff_session`).
- Microsoft Entra ID OIDC client with PKCE state management.
- Credential token encryption helper (`token_enc`) and reversible migration `0019_credential_tokens_encrypted.sql`.

### Phase 7b: API Server Endpoints & Security Boundaries (Tasks 6–9)
- OpenAPI contract update (spec-first) specifying `/v1/staff/*` endpoints and bumping version to 1.2.0.
- Password login (`POST /v1/staff/auth/password`) and logout (`POST /v1/staff/auth/logout`).
- Microsoft OAuth start and callback handlers with automated linking and provisioning.
- Staff self-service endpoints: profile completion, password change, device catalogue browsing, loans list, and credential display.
- Admin credential reveal endpoint with mandatory audit event creation.
- Strict route isolation: admin session cookies rejected on `/v1/staff/*`; staff cookies rejected on `/v1/*` admin routes.

### Phase 7c: Staff PWA Shell & First-Run Experience (Tasks 10–12)
- Staff PWA project configuration at `hdms-frontend/apps/staff` with manifest, service worker, and HTTPS dev environment.
- Staff login screen supporting both Microsoft SSO and employee number + password.
- Session guard in `authenticatedRoute` enforcing first-run employee number completion and mandatory temporary password changes.

### Phase 7d: Core Staff Experience (Tasks 13–14)
- Home screen with wake-locked accessible SVG QR code display and current loan list.
- Device catalogue listing with live search, availability status indicators, and expected return times (borrower identities are strictly withheld).

### Phase 7e: Settings & Lifecycle Management (Task 15)
- Staff settings route displaying user profile, assigned department, and active sign-in methods.
- Language switcher toggling between Japanese and English.
- Self-service password change with 12-character minimum validation.
- Clean sign out revoking the session server-side and purging the query cache.

---

## Exit Criteria

- [ ] **Both sign-in paths work on an iPhone with the app installed to the home screen.**
- [ ] **A self-signed-up person reaches a working QR without an administrator touching anything.**
- [ ] **An administrator can view and print any active user card, and every reveal appears in the audit log.**
- [ ] **A lost card can still be revoked and replaced.**
- [ ] **An admin cookie cannot reach `/v1/staff/*` and a staff cookie cannot reach `/v1/*` admin routes, both proven by tests.**
- [ ] **The kiosk borrow-and-return path is unchanged, proven by the existing test suite.**

---

## Risks & Mitigations

| Risk | Mitigation |
|---|---|
| Staff session mistaken for an administrative session | Dedicated `staffauth` realm, separate cookie names (`hdms_staff_session`), and mutual exclusion in HTTP middleware (ADR-0009). |
| Self-registration creates duplicate or orphan user records | Linking ladder checks email and employee number uniqueness before creating new users. |
| Reversible tokens expose credentials in database backups | Tokens encrypted under `HDMS_CREDENTIAL_ENC_KEY` held outside the database; no bulk export endpoint; audit row written for every reveal (ADR-0016). |
| Kiosk scanner exposed to user enumeration | Error bodies never disclose who holds a device; device detail displays only availability and return time. |
| Corridor kiosks used for unauthorized account creation | INV-11 preserved: kiosks have no user creation capabilities and cannot execute self-registration. |
