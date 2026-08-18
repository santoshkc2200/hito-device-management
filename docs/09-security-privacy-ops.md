# 09 — Security, privacy and operations

## Threat model

Realistic risks for an internal hospital asset-lending system. Not a
bank; not a toy either — it holds a staff roster and is a physical-access
adjacent system.

| # | Threat | Likelihood | Impact | Mitigation |
|---|---|---|---|---|
| T1 | Kiosk iPad stolen or removed | Medium | Medium | Kiosk token is narrowly scoped and instantly revocable; Guided Access lock; no admin capability; **no ability to create a user**; no bulk staff data readable from the kiosk |
| T2 | Someone borrows using a colleague's card | Medium | Low–Medium | Accepted risk. Card possession is the authentication factor, exactly as the paper register accepted a written name. Mitigation is audit + the ability to suspend. A PIN could be added but would destroy the < 8 s target |
| T2b | An unauthorised person obtains a borrowing account | Low | Medium | **Registration is administrator-only.** No self-service path exists, and the kiosk token cannot create a user. Every borrower was authorised by a named, accountable administrator, recorded in `registered_by` |
| T11 | Blank card stock stolen from the drawer | Low | Low | Unbound cards resolve as `unbound` and cannot borrow anything. They are only dangerous once an administrator binds one, which is a deliberate act. Keep the drawer locked; the console shows the unbound count so a shortfall is visible |
| T3 | Found/stolen card used after loss | Medium | Low | Reissue revokes it immediately; revoked-card scans are logged and surfaced on the dashboard |
| T4 | Fake barcode printed to impersonate a user | Low | Medium | 50-bit random tokens are not guessable; tokens are not derived from employee numbers; check character rejects malformed input |
| T5 | Database backup leaked | Low | Medium | Tokens stored as HMAC with a pepper held outside the database, so a backup alone yields no working cards; backups encrypted at rest |
| T6 | Admin account compromised | Low | High | Password + TOTP, session expiry, RBAC, append-only audit that the app role cannot alter |
| T7 | Staff-location oracle — scanning devices to learn who is where | Low | Medium | Error bodies never carry a holder's name, only a department; names appear only in-session at the kiosk |
| T8 | SQL injection / injection generally | Low | High | sqlc-generated parameterised queries throughout; no string-built SQL anywhere |
| T9 | Kiosk token exfiltrated from the iPad's storage | Low | Medium | Scoped token, rotatable, tied to a kiosk record that can be disabled; rotation on a schedule |
| T10 | Denial of service via a stuck scanner trigger | Medium | Low | Per-token rate limits, client debounce, server idempotency |
| T12 | An admin backdates or fabricates a loan to shift blame for a lost device | Low | Medium | Backfill is admin-only and fully audited with actor, `recorded_at` and slip reference; `origin` is immutable (INV-15); the physical page is retained and referenced by `paper_ref`, so any record can be checked against ink |
| T13 | Paper slips lost or discarded before being typed in | Medium | Low–Medium | Structured pads with page numbers and an "all rows entered" tick; a dashboard warning after 48 h; a named daily task in the morning runbook |

**Deliberately accepted:** T2. This is worth being explicit about with the
hospital. The system authenticates a *card*, not a *person*, which is precisely
the level of assurance the paper register provided — and it is a large
improvement, because it produces a tamper-evident timestamped record rather than
a signature nobody verifies. If the hospital later wants stronger assurance, the
credential model already supports a second factor (PIN on the kiosk) without
schema changes.

## Authentication and authorisation

**Admin login.** Argon2id password hashing, TOTP second factor, server-side
sessions in Postgres with `HttpOnly; Secure; SameSite=Lax` cookies, 12-hour
expiry with sliding renewal, and CSRF protection via the double-submit pattern on
state-changing requests. OIDC against hospital Active Directory is the intended
Phase 6 upgrade; the `auth` platform package is written with a pluggable
authenticator so this is an addition, not a rewrite.

**Kiosk.** An opaque 256-bit bearer token issued when the kiosk is registered,
stored hashed server-side. Scoped to exactly: create/advance/close sessions,
borrow, return, and read the minimum device and user data needed to render a
screen. **It has no user-creation, user-listing or user-modification capability
at all** (FR-45) — making registration administrator-only removed the one
capability that previously made a stolen kiosk interesting. Rotatable from the
admin console without reinstalling anything on the iPad. Kiosk requests are
rate-limited and their source IP recorded.

**Authorisation.** Enforced in HTTP middleware from a role→permission map, and
re-asserted in the module layer for anything destructive. Every check failure
produces an audit row — repeated `forbidden` responses for one actor is a signal.

## Privacy

**Data held:** staff name, employee number, department, work email/phone,
borrowing history, and — for paper-origin records — the register page reference
and the identity of the administrator who typed it in. Signatures stay on paper
and are never photographed or stored. **No patient data. No health data. No home addresses. No
national ID numbers.** The schema has no column for them and code review should
reject any attempt to add one without an explicit decision.

**Minimisation on the kiosk.** The kiosk screen shows the borrower's name and
their own open loans, and nothing else. It cannot list users, search staff, or
create them. The screen clears on a short timer so the next person in the queue
sees nothing.

**No use of the hospital's access-control cards in v1.** Staff ID cards carry
RFID/NFC, but this system does not read them and holds no data from them. If they
are adopted in Phase 6, the only datum stored will be the card UID, held as a
keyed hash exactly like any other credential — never the cardholder record from
the access-control system.

**Retention.** Default policy, to be confirmed with hospital compliance (Q7):

| Data | Retention |
|---|---|
| Open loans | Indefinite while open |
| Closed loan history | 3 years, then anonymised (user reference replaced with a department-level marker) |
| Audit events | 3 years |
| `scan_events` | 90 days |
| Physical register pages | 1 year, filed by page number matching `paper_ref` (Q12) |
| Archived users | Retained while any loan history references them; anonymised with it |

A scheduled job enforces this and records what it did — retention that nobody can
prove ran is not a policy.

**Transparency.** A printed notice at the kiosk stating what is recorded and why.
Cheap, and the right thing when staff movements are being logged.

## Deployment

Target: an on-premise Linux VM inside the hospital network (Q6). Compose is the
right size here — Kubernetes for one Go binary and one Postgres would be a
liability, not a capability.

```
┌─────────────────────────── Hospital network ───────────────────────────┐
│                                                                        │
│   iPad kiosk ──┐                                                       │
│   Admin PCs ───┼──▶ Caddy (TLS, internal CA) ──▶ hdms-api (Go)        │
│                │                                       │               │
│                                                        ▼               │
│                                                  PostgreSQL 18         │
│                                                        │               │
│                                                        ▼               │
│                                          nightly backup → NAS/offsite  │
└────────────────────────────────────────────────────────────────────────┘
```

- **Caddy** as the reverse proxy: automatic certificate management, HTTP/2, and a
  one-page config. With an internal CA, certificates are issued for
  `hdms.hospital.local` and the CA root is pushed to the iPads.
- **TLS is not optional.** Beyond the obvious, `getUserMedia` — the camera
  fallback — refuses to run on plain HTTP. A self-signed certificate the iPad
  does not trust will silently break the camera path. Get this right in Phase 0,
  not in Phase 5.
- **Frontend assets** are built to static files and served by Caddy. Two hosts or
  two paths: `/` for the kiosk, `/admin` for the console.
- **Migrations** run on startup from the embedded goose set, guarded by an
  advisory lock so concurrent starts cannot race.
- **Configuration** entirely by environment variables, twelve-factor style.
  Secrets (`DATABASE_URL`, `TOKEN_PEPPER`, `DEVICE_TOKEN_KEY`, `SESSION_KEY`)
  come from a root-only file, never from the compose file in git.

**Losing `TOKEN_PEPPER` invalidates every credential in the system.** It must be
backed up separately from the database — if both live in the same backup, the
pepper provides no protection; if it lives nowhere, a restore is useless. Store
it in the hospital's password manager and document the location in the runbook.

## Backup and recovery

| What | How | Frequency | Retention |
|---|---|---|---|
| Database | `pg_dump -Fc`, gzipped, encrypted | Nightly 02:00 | 30 daily, 12 monthly |
| WAL archive | Continuous archiving (optional, if RPO must beat 24 h) | Continuous | 7 days |
| Secrets (pepper, keys) | Manual, to the hospital password manager | On change | Current + previous |
| Configuration | In git | On change | Forever |

**A restore drill is a Phase 5 exit criterion.** Restoring into a scratch
database, verifying loan counts and a sample of credential resolutions, and
timing the whole thing. A backup that has never been restored is a hypothesis.

## Observability

**Logs** — `log/slog` in JSON, with request ID, actor, module, and duration on
every request. Credential tokens are never logged: the logging middleware has an
explicit denylist for the fields carrying them, and a test asserts a token value
never appears in log output.

**Metrics** — Prometheus format at `/metrics` (internal only):

- `hdms_loans_open` (gauge), `hdms_devices_by_status` (gauge)
- `hdms_transactions_total{action,source,outcome}` (counter)
- `hdms_scan_rejections_total{reason}` (counter)
- `hdms_http_request_duration_seconds{route,status}` (histogram)
- `hdms_kiosk_last_seen_seconds{kiosk}` (gauge)
- `hdms_session_expired_total` (counter)

**Traces** — OpenTelemetry spans across HTTP → module → database, sampled at
100% given the trivial volume. Debugging "the kiosk was slow at 09:14" becomes
looking at one trace.

**Alerts** that actually warrant waking someone:

| Alert | Condition |
|---|---|
| API down | `/readyz` failing 2 min |
| Database unreachable | Connection failures > 1 min |
| Kiosk offline | No contact for 30 min during working hours |
| Backup failed | Nightly job did not report success |
| Disk > 85% | Standard |
| Scan rejection spike | Rejections > 30% of scans over 15 min — usually a broken scanner or a bad label batch |
| Reconciliation mismatch | Nightly check found `devices.status` disagreeing with `loans` (INV-3) |
| Paper backlog | No paper page recorded for 48 h during working days |

Alerts route to hospital IT's existing channel. If there is none, email to a
shared mailbox — an alert nobody receives is not an alert.

## Runbooks

Written in Phase 5, living in `docs/runbooks/`. The list is chosen from what will
actually happen:

1. Scanner not reading — battery, pairing, setup barcodes, camera fallback
2. Kiosk shows "reconnecting" — network, API, certificate checks
3. Device stuck showing as on loan — reconciliation and force-return
4. Staff member lost their card — reissue and print
5. Register a new borrower and issue their card
6. **Daily: type in yesterday's paper register page, then issue cards to anyone
   new** — the single routine that keeps the record complete
7. New device arrives — register, label, verify
8. Device retired or lost — status change and write-off
9. Resolve a backfill custody conflict
10. Restore the database from backup
11. Rotate a kiosk token
12. Register a new kiosk
13. Replenish blank card stock and paper register pads
14. Monthly: verify backups, check overdue list, review the audit log, review the
    transactions-by-origin trend

## Compliance posture

This system is **not a medical device** and holds no patient data — a point worth
stating in writing to the hospital, because "hospital software" triggers
assumptions. It is staff-administrative software, subject to the hospital's
internal information-security policy and local data-protection law for employee
records.

Concrete obligations to confirm with hospital compliance (Q7): lawful basis for
processing employee records, retention limits, staff notification, and whether
any local regulator requires registration for employee monitoring. The retention
job, the audit log, and the minimised schema are designed to make the answers
easy rather than to guess at them.
