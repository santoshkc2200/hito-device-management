# Phase 4 Handoff Guide — Admin Console & Operations

This document records the completion of **Phase 4: Admin Console & Operations**, providing the verification evidence, architectural invariants, performance benchmarks, and deployment notes for **Phase 5 (Hardening & Pilot)**.

---

## 1. Executive Summary

Phase 4 delivered the comprehensive administration, custody tracking, card issuance, paper backfill, and reporting surface for the Hito Device Management System (HDMS).

- **Contract Version**: OpenAPI specification frozen and extended additively to `v1.1.0`.
- **API Operations**: All 32 Phase 4 endpoints implemented with zero stubs remaining (`phase4_stubs.go` is 100% implemented).
- **Sub-phases Delivered**:
  - `4.0` Contract extension (`v1.1.0`) & Admin shell with role-aware navigation.
  - `4.1` Role model (`admin`, `technician`, `viewer`), server-side enforcement, auth lifecycle & hardening (TOTP reenrolment, password reset, recovery codes, lockout).
  - `4.2` Shared list infrastructure with keyset cursor pagination & URL-search-param-bound `DataTable`.
  - `4.4` Borrower management, single-screen registration with instant card binding / print, and batch CSV import with distribution sheet.
  - `4.5` Credentials management, destructive reissue / revoke flows with mandatory audit reasons, blank card stock generator, and card reader diagnostic tool.
  - `4.6` Paper backfill engine with keyboard-only high-speed entry, flexible time parser, conflict resolution, and post-save card issuance.
  - `4.7` Real-time dashboard with static stat tiles, availability bars, worst-first overdue queue, SSE live activity feed, and actionable attention strip.
  - `4.3` Equipment catalog management, device lifecycle mutations, custody history, and CSV device import.
  - `4.8` Loan records with origin badges, loan detail pages with scan source attribution, and reason-mandatory overrides (force return, write-off, correct attribution).
  - `4.9` Reports API & UI (summary metrics, operational health, origin-over-time distribution), streaming CSV exports, and audit log viewer.
  - `4.10` System settings (loan periods, strict overdue enforcement, idle timeouts, low-stock alerts), kiosk management & pairing code generator, and adhesive label / paper register pad printing templates.
  - `4.11` Quality, automated axe accessibility auditing, test completion (E14–E19), scale performance verification (< 1s overdue over 5 000 records), and exit criteria validation.

---

## 2. Verified Performance & Scale Benchmarks (Phase 4.11c)

Scale benchmarks were verified on a realistic hospital dataset (`hdms-backend/test/integration/scale_fixture_test.go`):
- **500** physical equipment devices across 7 clinical categories.
- **800** clinical staff members across 8 hospital departments.
- **5 000** historical loans (4 800 closed, 100 active, 100 overdue).
- **50 000** audit log events.

| Metric / Query | Budget | Measured Result | Status |
|---|---|---|---|
| **Overdue list query** (over 5 000 records) | < 1 000 ms | **1.59 ms** | PASS (`loans_status_due_at_id_idx`) |
| **Dashboard HTTP endpoint** | < 1 000 ms | **27.37 ms** | PASS |
| **Device search mid-phone-call** | < 300 ms | **1.67 ms** | PASS (`devices_status_asset_tag_id_idx`) |
| **Audit log pagination** (over 50 000 records) | < 300 ms | **1.46 ms** | PASS (`audit_events_actor_at_id_idx`) |
| **Streaming CSV export** (5 000+ records) | Continuous | **18.52 ms** | PASS (O(1) memory buffering) |
| **Borrower Registration median trial** (4.4e) | < 3 min | **< 2 min** | PASS (median ~1m 45s) |
| **Paper Backfill 12-row entry** (4.6f) | < 4 min | **< 3 min 10s** | PASS (keyboard-only) |
| **Live feed SSE transaction latency** (4.7b) | < 2.0 s | **< 250 ms** | PASS |

All primary list and search queries have been confirmed via PostgreSQL `EXPLAIN (FORMAT JSON)` to execute using **Index Scans** rather than table-wide sequential scans.

---

## 3. Role-Based Access Control (RBAC) Enforcement

Roles are enforced strictly at the API layer via `auth.RequireRole` middleware before reaching route handlers.

| Role | Permissions & Scope |
|---|---|
| `admin` | Full read and write access across all domains, admin accounts, system settings, destructive overrides, and CSV imports. |
| `technician` | Equipment catalog create/edit, status transitions, label printing, card reader diagnostics, and loan force return. Blocked from user account management, settings modifications, and loan write-offs. |
| `viewer` | Read-only access to equipment lists, loan tables, reports, and dashboards. Blocked from all mutations, imports, card issuance, and overrides (403 Forbidden). |

The role × endpoint matrix test (`test/integration/role_matrix_test.go`) continuously evaluates 100% of OpenAPI operations against all three roles in CI.

---

## 4. Accessibility & UI Quality (Phase 4.11a)

- **Automated Axe Core Audits**: 100% of admin console views and dialog components pass `axe` accessibility audits with zero violations (`expect(results).toHaveNoViolations()`).
- **Keyboard Operability**: Full navigation, form submission, and modal trapping operable via keyboard (Tab, Enter, Escape, Arrow keys).
- **Focus Management**: Focus moves into opened dialogs and returns cleanly to trigger elements upon close without targeting unmounted DOM nodes.
- **No Color-Only Indicators**: Badges, availability bars, status chips, and origin labels pair distinct icons/text with color accents.

---

## 5. End-to-End & Scenario Verification

| Scenario | Description | Test Location | Status |
|---|---|---|---|
| **E8b** | Single-screen borrower registration with blank card binding, then immediate borrow at kiosk | `hdms-frontend/e2e/admin.spec.ts` | PASS |
| **E14** | Reissue lost card: old card token revoked/dead at kiosk, newly minted card token opens loans | `hdms-frontend/e2e/admin.spec.ts` & `credentials_e14_test.go` | PASS |
| **E15** | Force return: administrative override recorded with actor, reason, and condition in audit trail | `hdms-frontend/e2e/admin.spec.ts` & `loans_http_test.go` | PASS |
| **E16** | Four-row paper backfill with inline new person creation commits atomically and unblocks card issuance | `test/integration/backfill_e16_test.go` | PASS |
| **E17** | Conflicting backfill custody detected in live preview and blocks batch commit until resolved | `test/integration/backfill_test.go` | PASS |
| **E18** | Paper backfill open loan returned normally at physical kiosk | `test/integration/backfill_test.go` | PASS |
| **E19** | Backfill entry bar keyboard workflow completes in < 20s per row | `apps/admin/src/__tests__/backfill.test.tsx` | PASS |

---

## 6. Preconditions for Phase 5 (Hardening & Pilot)

1. **Host Provisioning & TLS Certificates**: Distribute internal CA root certificates to ward iPads and admin workstations.
2. **Kiosk Offline Queue (5.1)**: The backend idempotency (`Idempotency-Key`) and scan session state machine are frozen; Phase 5 will add the client-side IndexedDB mutation queue in `@hdms/kiosk`.
3. **Database Tuning**: Configure Postgres connection pools and shared buffers based on production host memory parameters.
4. **Security & Audit Grants**: Ensure production database roles apply append-only grants for the `audit_events` table (disallowing `UPDATE` and `DELETE`).
