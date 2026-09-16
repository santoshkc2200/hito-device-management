# ADR-0012 — Relaxing administrator-only registration for Entra ID self-signup

**Status:** Accepted · **Date:** 2026-09-15 · **Supersedes part of** [ADR-0003](0003-credential-as-separate-entity.md) and [docs/00](00-product-overview.md)

## Context

A founding architectural invariant of HDMS was that **registration is administrator-only** (INV-11). Walk-up kiosk terminals could borrow and return devices, but could never create a borrower record. Every user in the system was registered by a named administrator who issued them a physical barcode card. The `registered_by` column strictly enforced this: only `admin:<id>`, `import`, and `import:<batch>` were valid provenances.

In Phase 7, the hospital requested staff self-service: staff members should be able to open the Staff PWA on their own smartphones, authenticate with their hospital Microsoft Entra ID account, and immediately obtain their borrower QR code without waiting in line at an administrative office.

Self-service onboarding directly challenges the founding constraint. We needed to decide whether to allow self-registration, how to maintain auditability, and how to prevent rogue account creation.

## Decision

We accept **`self:microsoft`** as an authorized registration provenance:

1. **Entra ID tenant membership as the authorizing authority:** We delegate the authorization decision to the hospital's Microsoft Entra ID tenant. If a user can authenticate against the hospital's designated tenant ID and possesses an approved domain email, they are permitted to self-provision an account in HDMS.
2. **Provenance tracking:** The user record is created with `registered_by = 'self:microsoft'`. The external subject (`oid`) is immutably linked in `staff_identities`.
3. **Two-phase completion for missing employee numbers:** When a user self-signs up via Microsoft Entra ID, their directory claims might not carry their hospital employee number. The account is created with `profile_complete = false` and a deterministic placeholder employee number (`MS-<hash>`). The session guard redirects the user to complete their profile before they can access their QR code, devices, or loans.
4. **INV-11 remains absolute on kiosks:** The walk-up kiosk terminal still **never** creates a user. The kiosk bearer token has no user creation capability, and any `registered_by` starting with `kiosk:` is rejected with `ErrRegisteredByInvalid`.

## Consequences

**What is given up (the costs)**
- **No universal administrator vetting:** Hospital operations can no longer assume that every record in the `users` table was manually reviewed and verified by a physical hospital administrator. An active employee in the hospital directory can create an HDMS user profile autonomously.
- **Temporary placeholder data:** The `employee_no` column temporarily stores synthetic identifiers (`MS-...`) for incomplete self-signups until the user submits their true employee number.
- **Directory dependency for fraud control:** If a departing employee's Entra ID account is not promptly disabled or suspended by hospital IT, they retain the ability to self-register or sign in until their directory status changes.

**What did not change (the invariants preserved)**
- **Kiosks remain unprivileged:** Walk-up iPads in hospital corridors cannot create accounts. A compromised kiosk token cannot be used to forge borrowers.
- **Custody records remain strict:** Borrowing and returning equipment still require an active, resolved credential. A self-registered user cannot borrow until their profile is complete and their credential token is minted.
- **Auditability:** Every user record clearly records whether it originated from manual administrator entry, bulk CSV import, or self-registration via Microsoft SSO.

## Alternatives

- **Pre-registration only (link on first login):** Require administrators to pre-load all employee numbers and names, with Microsoft SSO only matching existing records. Rejected: the hospital's initial roster data is incomplete, and requiring manual pre-registration defeats the goal of zero-attendant onboarding for new staff.
- **Allowing kiosk-based self-registration:** Rejected. Corridors are unmonitored; allowing anyone at a kiosk to create a borrower account would destroy custody integrity.

## What would make us revisit this

If the hospital establishes an automated HR roster sync (Phase 6.3b) that reliably provisions and synchronizes all staff records directly from the central HRMS, eliminating the need for self-signup.
