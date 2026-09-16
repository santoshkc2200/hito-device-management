# HDMS — Hito Device Management System

Documentation set for the hospital device lending system: staff borrow and return
shared equipment (laptops, pendrives, projectors, etc.) by scanning a barcode on
the device and a barcode on their card at an iPad kiosk.

Two constraints shape the design, both set by the hospital:

- **Registration is administrator-only.** The kiosk borrows and returns; it never
  creates a user. A staff member is registered in the admin console and handed a
  card.
- **The existing RFID/NFC staff ID cards are not available to this project yet.**
  v1 issues its own QR cards; adopting the RFID cards is a planned Phase 6
  integration that the credential model is already built for.
- **The paper register stays, as the overflow lane.** When someone has no card
  yet, or anything is down, the attendant writes it on paper exactly as today and
  an administrator types it in later. The counter never stops.

## Read in this order

| # | Document | What it answers |
|---|---|---|
| 00 | [Product overview](00-product-overview.md) | Why this exists, who uses it, what is in and out of scope |
| 01 | [Requirements](01-requirements.md) | Functional requirements, quality attributes, open questions |
| 02 | [Architecture](02-architecture.md) | Modular monolith design, module map, technology choices |
| 03 | [Domain model](03-domain-model.md) | Entities, schema, invariants, state machines |
| 04 | [Scanning & checkout flows](04-scanning-and-checkout-flows.md) | The order-agnostic scan session state machine |
| 05 | [Credentials & labeling](05-credentials-and-labeling.md) | Token format, symbology, printing, lost-card policy |
| 06 | [API contract](06-api-contract.md) | Endpoint design, errors, idempotency, realtime |
| 07 | [Kiosk app](07-kiosk-app.md) | iPad PWA, scanner input, camera fallback, UX |
| 08 | [Admin console](08-admin-console.md) | Management screens and workflows |
| 09 | [Security, privacy & operations](09-security-privacy-ops.md) | AuthN/Z, PII, audit, deploy, backup, monitoring |
| 10 | [Testing strategy](10-testing-strategy.md) | Test pyramid, tooling, what must be proven |

## Delivery phases

Each phase has its own plan with tasks, deliverables and exit criteria.

| Phase | Title | Focus | Est. |
|---|---|---|---|
| 0 | [Foundations](phases/phase-0-foundations.md) | Repo, tooling, CI, schema pipeline, skeletons | ~1 wk |
| 1 | [Identity, catalog & credentials](phases/phase-1-identity-catalog-credentials.md) | Staff registration, devices, barcode issuing and printing | ~2 wk |
| 2 | [Lending & checkout engine](phases/phase-2-lending-checkout.md) · [sub-phases](phases/phase-2/) | Loans, the scan session state machine | ~2 wk |
| 3 | [Kiosk application](phases/phase-3-kiosk-app.md) · [sub-phases](phases/phase-3/) | iPad PWA, scanner + camera, borrow/return | ~2 wk |
| 4 | [Admin console](phases/phase-4-admin-console.md) · [sub-phases](phases/phase-4/) | Registration, paper backfill, monitoring, reissue, reports | ~2–3 wk |
| 5 | [Hardening & pilot](phases/phase-5-hardening-pilot.md) · [sub-phases](phases/phase-5/) | Security, backups, offline, UAT, rollout | ~2 wk + 2 wk pilot |
| 6 | [Extensibility & roadmap](phases/phase-6-extensibility.md) · [sub-phases](phases/phase-6/) | **Adopt the RFID/NFC ID cards**, notifications, multi-location | ongoing |
| 7 | [Staff identity, self-signup & staff PWA](phases/phase-7-staff-identity.md) | Staff login (Microsoft SSO / password), staff PWA (QR, loans, device catalogue), reversible token storage | ~2 wk |

Estimates assume one full-time developer. Phases 3 and 4 can run in parallel once
Phase 2 freezes the API contract. Total to pilot: **~10–11 weeks**.

## Architecture decision records

- [ADR-0001 — Modular monolith in Go](adr/0001-modular-monolith-go.md)
- [ADR-0002 — Postgres with sqlc and pgx](adr/0002-postgres-sqlc-pgx.md)
- [ADR-0003 — Credentials as a separate entity from users and devices](adr/0003-credential-as-separate-entity.md)
- [ADR-0004 — Order-agnostic scan session as an explicit state machine](adr/0004-scan-session-state-machine.md)
- [ADR-0005 — QR (2D) as the default symbology](adr/0005-qr-default-symbology.md)
- [ADR-0006 — Spec-first OpenAPI with generated server and client](adr/0006-spec-first-openapi.md)
- [ADR-0007 — Web PWA kiosk instead of a native iPad app](adr/0007-pwa-kiosk-not-native.md)
- [ADR-0008 — Temporal exclusion constraint for device custody](adr/0008-temporal-custody-constraint.md)
- [ADR-0009 — Dedicated staff authentication realm](adr/0009-staff-authentication-realm.md)
- [ADR-0012 — Relaxing administrator-only registration for Entra ID self-signup](adr/0012-relaxing-administrator-only-registration.md)
- [ADR-0016 — User credential tokens stored reversibly](adr/0016-user-credential-tokens-stored-reversibly.md)

## Repository layout

```
hito-device-management/
├── docs/                 # this documentation set
├── hdms-backend/         # Go modular monolith
└── hdms-frontend/        # pnpm workspace: kiosk app, admin app, staff app, shared packages
```
