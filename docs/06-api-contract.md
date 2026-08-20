# 06 — API contract

## Principles

- **Spec-first.** `hdms-backend/api/openapi.yaml` is the source of truth. Go
  server interfaces are generated with `oapi-codegen`; the TypeScript client is
  generated into `packages/api-client`. Neither is hand-edited.
  ([ADR-0006](adr/0006-spec-first-openapi.md))
- **Versioned path prefix** `/v1`. A breaking change means `/v2`, not a silent
  field change — kiosks in the field may lag a deploy.
- **Typed errors** via RFC 9457 `application/problem+json`. The kiosk branches on
  a machine-readable `type`, never on a message string.
- **Idempotent writes.** Every mutating endpoint accepts `Idempotency-Key`.
- **No PII in URLs.** IDs only; never employee numbers or names in a path.

## Authentication

Three principals, three mechanisms.

| Principal | Mechanism | Scope |
|---|---|---|
| **Kiosk** | `Authorization: Bearer <kiosk-token>` — a long-lived opaque token issued when the kiosk is registered, rotated on demand | Create sessions, submit scans, borrow and return, read the minimum device/user data needed to render a screen. **It cannot create, modify or list users, and has no admin capability whatsoever** (FR-45). |
| **Admin user** | Session cookie (`HttpOnly`, `Secure`, `SameSite=Lax`) after password + TOTP login | Everything permitted by their role |
| **Service/CLI** | `Authorization: Bearer <service-token>` | Imports, exports, scheduled jobs |

The kiosk token deserves emphasis: an iPad in a public corridor is a device that
can walk away. Its token is scoped so that the worst case of theft is someone
recording bogus loans at a kiosk — not exfiltrating the staff directory or
deleting devices. Admin can revoke a kiosk instantly, and the token is rotated on
a schedule.

Making registration administrator-only tightened this further: the kiosk token
previously needed a user-creation capability for self-enrollment, and now has
none at all. A stolen kiosk cannot manufacture a borrower.

## Endpoints

### Admin auth

```
POST   /v1/auth/login     { email, password, totpCode | recoveryCode } → admin identity; sets hdms_session (HttpOnly) + hdms_csrf cookies
POST   /v1/auth/logout    revoke the current session
GET    /v1/auth/me        the identity of the currently authenticated admin

POST   /v1/auth/password        { currentPassword, newPassword }  own password; revokes other sessions
POST   /v1/auth/totp/reenrol    → new secret ONCE; the old one stays valid until confirm
POST   /v1/auth/totp/confirm    { totpCode }   kills the previous secret
POST   /v1/auth/recovery-codes  → new codes ONCE, stored hashed, single use each

GET    /v1/admins
POST   /v1/admins                    → account + TOTP enrolment + recovery codes, ONCE
GET    /v1/admins/{id}
PATCH  /v1/admins/{id}               name, role, status; email is not editable
POST   /v1/admins/{id}/reset-password { password, reason }
POST   /v1/admins/{id}/reset-totp     { reason } → new secret ONCE — the lost-phone path
POST   /v1/admins/{id}/unlock         { reason } clear a failed-login lockout
```

`recoveryCode` stands in for `totpCode` when the authenticator is gone, which is
why `totpCode` is optional in the schema rather than required — one of the two
must be present, and the server says the same thing either way when neither is.

No self-service enrollment endpoint: the first admin account (and its TOTP
secret) is created by `hdms-cli admin bootstrap`, run once by whoever deploys
the system — consistent with registration being administrator-only everywhere
else (FR-45's spirit). Every other `/v1/*` route (except `/healthz`, `/readyz`
and `/auth/login` itself) requires the `adminSession` cookie; mutating
methods additionally require the `X-CSRF-Token` header to match the
`hdms_csrf` cookie (double-submit).

### Session and checkout — the kiosk's hot path

```
POST   /v1/sessions                        create a scan session
GET    /v1/sessions/{id}                   current state (recovery after reload)
POST   /v1/sessions/{id}/scan              submit a scanned token   ★
POST   /v1/sessions/{id}/return-loan       return by tapping, not scanning
POST   /v1/sessions/{id}/close             finish ("Done")
DELETE /v1/sessions/{id}                   cancel
```

`POST /v1/sessions/{id}/scan` is the endpoint that carries the whole product.

**Request**
```json
{
  "token":  "HD-U-7K3M9QXA2F-4",
  "source": "scanner",
  "scannedAt": "2026-08-18T09:14:22.183Z"
}
```

**Response** — always the *complete* new session state plus what just happened,
so the kiosk never has to reconstruct anything:

```json
{
  "session": {
    "id": "0192f3c1-…",
    "state": "ready",
    "user": {
      "id": "0192a1…", "fullName": "Dr. A. Sharma",
      "department": "Radiology", "openLoanCount": 2
    },
    "pendingDevice": null,
    "expiresAt": "2026-08-18T09:14:47Z"
  },
  "outcome": {
    "kind": "borrowed",
    "loanId": "0192b7…",
    "device": { "id": "0192c…", "assetTag": "LAPTOP-07",
                "name": "Dell Latitude 5420" },
    "dueAt": "2026-08-19T09:14:22Z"
  },
  "openLoans": [ … ],
  "message": { "title": "Borrowed", "detail": "Due tomorrow 9:14 AM",
               "tone": "success" }
}
```

`outcome.kind` is the discriminator the kiosk switches on:

| `kind` | Meaning |
|---|---|
| `device_pending` | Device accepted, waiting for a user |
| `user_identified` | User accepted, waiting for a device |
| `borrowed` | A loan was opened |
| `returned` | A loan was closed |
| `rejected` | Nothing happened; `message` explains why — including an unknown, unbound or revoked card, which directs the person to the administrator |
| `duplicate` | Debounced repeat scan; ignore |

Rejections come back as `200` with `kind: "rejected"`, **not** as an HTTP error.
"This device is on loan to someone else" is a normal business outcome of a
successful request, and modelling it as a `4xx` would force the kiosk to parse
errors to drive its main UI. HTTP errors are reserved for genuine faults: bad
token format, expired session, auth failure, server error.

The `message` object is server-authored and display-ready, with `title`, `detail`
and `tone` (`success` | `info` | `warning` | `error`). Wording lives on the
server so it can be corrected — or translated — without shipping a new kiosk
build to every iPad.

### Catalog and identity (admin)

```
GET    /v1/devices                  ?status= &category= &q= &cursor=
POST   /v1/devices
GET    /v1/devices/{id}
PATCH  /v1/devices/{id}
POST   /v1/devices/{id}/status      { status, reason }
GET    /v1/devices/{id}/loans

GET    /v1/users                    ?status= &department= &q= &hasCredential= &cursor=
POST   /v1/users                    register a borrower; admin-only
POST   /v1/users/register-with-card  register + bind a card atomically; admin-only
GET    /v1/users/{id}
PATCH  /v1/users/{id}
POST   /v1/users/{id}/suspend       { reason }
POST   /v1/users/{id}/archive       { reason }   refused while they still hold a device
GET    /v1/users/{id}/loans
GET    /v1/users/check-employee-no  ?employeeNo= → { available, existingUserId? }   live duplicate check (FR-41)

GET    /v1/departments              picker for the registration form's department field

GET    /v1/categories
POST   /v1/categories
PATCH  /v1/categories/{id}
```

### Credentials

```
GET    /v1/credentials                       ?subjectType= &subjectId= &status=
POST   /v1/credentials                       issue a new one   → token returned ONCE
POST   /v1/credentials/blank-batch           { count, kind } → pre-printed card stock
GET    /v1/credentials/unbound-count         how many blank cards remain unbound
GET    /v1/credentials/resolve               ?token= → subject/status, for binding a scanned physical card
POST   /v1/credentials/{id}/bind             bind an unbound card to a user; admin-only
POST   /v1/credentials/{id}/reprint          device tokens only; returns the token
POST   /v1/credentials/{id}/revoke           { reason }
POST   /v1/credentials/{id}/reissue          { reason } → revokes old, returns new token
GET    /v1/credentials/{id}/history
```

`POST /v1/credentials` and `/reissue` are the only responses in the system that
ever contain a plaintext token. They are marked `x-sensitive: true` in the spec,
excluded from request/response logging by an explicit middleware allowlist, and
sent with `Cache-Control: no-store`.

### Loans and reporting

```
GET    /v1/loans                    ?status= &origin= &userId= &deviceId= &from= &to= &cursor=
GET    /v1/loans/{id}
POST   /v1/loans/{id}/force-return  { reason, conditionIn, returnedAt? }  admin override
POST   /v1/loans/{id}/write-off     { reason }                device declared lost
POST   /v1/loans/{id}/correct-attribution { userId, reason }  reassign to who actually holds it
GET    /v1/reports/summary          counts by status, overdue, utilisation
GET    /v1/reports/by-origin        ?from= &to= &bucket=day|week|month   (FR-78)
GET    /v1/reports/disputed         records forced past a custody conflict
GET    /v1/reports/operational-health  manual-entry, camera-fallback and rejection counts
GET    /v1/reports/loans.csv        streaming CSV export, same filters as GET /loans
GET    /v1/reports/devices.csv      streaming CSV export, same filters as GET /devices
GET    /v1/reports/users.csv        streaming CSV export, same filters as GET /users
```

Every export takes the same filters as the list it mirrors, so what is exported
is what is on screen, and each is streamed rather than buffered — a multi-year
export must not hold memory.

### Paper backfill

```
POST   /v1/backfill/preview         validate a batch, return per-row resolution + conflicts
POST   /v1/backfill                 commit a validated batch atomically
GET    /v1/backfill/last-entry      when a paper page was last recorded (dashboard nag)
```

`POST /v1/backfill/preview` is what makes the admin screen feel instant: it takes
the whole staged batch and returns, per row, the **auto-detected action**
(FR-74), the resolved device and person, and any custody conflict — without
writing anything. The screen calls it as rows change, so a conflict surfaces while
the admin is still looking at the paper page, not after they press Save.

**Request**
```json
{
  "paperRef": "2026-08-18 p.3",
  "rows": [
    { "clientRowId": "r1", "deviceRef": "LAPTOP-07", "userRef": {"userId": "0192a1…"},
      "borrowedAt": "2026-08-15T09:15:00+05:45", "returnedAt": "2026-08-18T14:30:00+05:45" },
    { "clientRowId": "r2", "deviceRef": "PENDRIVE-08",
      "userRef": {"newUser": {"fullName": "Bimala Lama", "employeeNo": "HH-2407",
                              "departmentId": "0192d…"}},
      "borrowedAt": "2026-08-17T11:20:00+05:45" }
  ]
}
```

`deviceRef` accepts an asset tag or a scanned credential token — the same field
in the UI serves both, so a USB scanner on the admin PC fills it with one trigger
pull. `userRef` is a discriminated union of `userId`, an employee number, a
scanned token, or a `newUser` object; the last one is how FR-73 is satisfied
without leaving the screen.

**Response** — one entry per row, keyed by `clientRowId` so the UI can update in
place without reordering the admin's work:

```json
{
  "rows": [
    { "clientRowId": "r1", "action": "return", "status": "ok",
      "device": {…}, "user": {…}, "closesLoanId": "0192b7…" },
    { "clientRowId": "r2", "action": "borrow", "status": "ok",
      "device": {…}, "createsUser": true },
    { "clientRowId": "r3", "action": "borrow", "status": "conflict",
      "conflict": {
        "type": "overlapping-custody",
        "existingLoan": { "id": "…", "userDisplay": "D. Karki",
                          "department": "Radiology",
                          "borrowedAt": "…", "returnedAt": "…", "origin": "kiosk" },
        "resolutions": ["truncate-existing", "change-device", "discard-row",
                        "record-as-disputed"]
      } }
  ],
  "summary": { "ok": 3, "conflicts": 1, "newUsers": 2 }
}
```

`POST /v1/backfill` commits the batch **in one transaction** — all rows or none.
It re-runs the same validation server-side; a preview is a convenience, never a
grant. It returns the created loan IDs and the IDs of any users created, which is
what the "issue cards to the 2 new people" step (FR-77) is driven from.

Backfill is `admin`-only. The kiosk token cannot reach it.

### Monitoring and administration

```
GET    /v1/dashboard                on-loan now, overdue, availability by category
GET    /v1/events/stream            Server-Sent Events: live loan + device changes
GET    /v1/audit                    ?actor= &subject= &action= &from= &to= &cursor=
GET    /v1/audit.csv                streaming CSV export, same filters
GET    /v1/kiosks
POST   /v1/kiosks                   register a kiosk → returns its token ONCE
GET    /v1/kiosks/{id}
PATCH  /v1/kiosks/{id}              name, location, enabled scan sources
POST   /v1/kiosks/{id}/rotate-token
POST   /v1/kiosks/{id}/disable
POST   /v1/kiosks/{id}/enable       token unchanged

GET    /v1/settings                 policy, label template, paper slip template
PATCH  /v1/settings                 each section present replaces that section wholesale

POST   /v1/imports/users/preview    text/csv → per-row outcome + previewId; writes nothing
POST   /v1/imports/users            { previewId }
POST   /v1/imports/devices/preview  text/csv → per-row outcome + previewId; writes nothing
POST   /v1/imports/devices          { previewId }
GET    /v1/healthz                  liveness (no auth)
GET    /v1/readyz                   readiness incl. DB (no auth)
```

**SSE, not WebSocket**, for live updates: the traffic is strictly server→client,
SSE reconnects automatically with `Last-Event-ID`, it survives proxies that
mangle WebSocket upgrades, and it needs no extra dependency in Go. The admin
dashboard subscribes; kiosks do not need it.

## Error format

```json
{
  "type":     "https://hdms.hito.local/errors/device-on-loan",
  "title":    "Device is already on loan",
  "status":   409,
  "detail":   "LAPTOP-07 is currently held by another staff member.",
  "instance": "/v1/sessions/0192f3c1/scan",
  "requestId": "01JCXYZ…",
  "extensions": { "deviceId": "0192c…", "holderDepartment": "Radiology" }
}
```

Registered error types:

| `type` suffix | HTTP | When |
|---|---|---|
| `invalid-token-format` | 400 | Checksum or namespace failed |
| `credential-revoked` | 409 | Scanned a reissued/dead card |
| `credential-unknown` | 404 | Token resolves to nothing |
| `credential-unbound` | 409 | A real but unissued blank card was scanned |
| `device-on-loan` | 409 | Borrow attempted on a held device |
| `overlapping-custody` | 409 | A backfilled loan would overlap an existing custody period for the same device (INV-13) |
| `backdated-not-permitted` | 422 | A non-backfill endpoint received a past timestamp |
| `device-unavailable` | 409 | Maintenance / retired / lost |
| `user-suspended` | 403 | Suspended borrower |
| `session-expired` | 410 | Session TTL elapsed |
| `session-conflict` | 409 | Concurrent modification of one session |
| `idempotency-mismatch` | 422 | Same key, different payload |
| `validation-failed` | 422 | `extensions.fields[]` names the offending fields |
| `unauthorized` / `forbidden` | 401 / 403 | Auth |
| `rate-limited` | 429 | With `Retry-After` |

Note that `credential-unknown` and `credential-unbound` reach the kiosk as
`outcome.kind: "rejected"` on a `200`, not as HTTP errors — they are ordinary
outcomes of a working system, and the kiosk renders the server-authored guidance
message. The HTTP error types exist for the admin API, where the same conditions
are genuine faults.

`extensions` deliberately never contains a holder's **name** in an error body —
only the department. The name appears in the `scan` endpoint's `message` for the
legitimate in-session case, where the borrower is standing at the kiosk and the
information is needed. This keeps "scan any device to learn who has it" from
becoming a staff-location oracle.

## Cross-cutting conventions

**Idempotency.** `Idempotency-Key` on every `POST`. Keys and their responses are
stored for 24 h. A replay with the same key returns the stored response with
`Idempotency-Replayed: true`; a replay with a *different* body returns
`idempotency-mismatch`. This is what makes the Phase 5 offline queue safe.

**Pagination.** Cursor-based (`?cursor=&limit=`), returning
`{ items, nextCursor }`. Offset pagination is not offered — history grows and
offsets skip rows when data changes underneath.

**Timestamps.** RFC 3339 with an explicit offset, always UTC on the wire. The
kiosk and admin render in hospital local time.

Backfill is the one place a client supplies a *past* timestamp, and it does so
with an explicit offset because the administrator is reading a wall-clock time
off a paper page. Every other endpoint takes its timestamps from the server
clock; passing `borrowedAt` to a live borrow is rejected with
`backdated-not-permitted` rather than silently honoured, so a bug or a tampered
kiosk cannot rewrite when something happened.

**Request correlation.** Every request gets an `X-Request-Id` (accepted from the
client or generated). It appears in logs, traces, audit rows, and error bodies —
so an attendant reading an error code off the kiosk screen gives support enough
to find the exact request.

**Rate limits.** Per kiosk token and per admin session, generous
(60 scans/minute) — enough that a stuck scanner trigger cannot generate load,
low enough not to interfere with real use.

## Contract workflow

```
edit api/openapi.yaml
   ↓
make generate
   ├─ oapi-codegen  → internal/platform/httpx/gen/  (Go server interfaces + types)
   └─ openapi-ts    → packages/api-client/src/gen/  (typed TS client + Zod schemas)
   ↓
Go compiler fails on unimplemented handlers
TS compiler fails on changed shapes in both apps
   ↓
implement, test, commit spec + generated code together
```

Generated code **is committed** so a checkout builds without a codegen step, and
CI verifies `make generate` produces no diff. A contract change that someone
forgot to regenerate is then a red build, not a runtime surprise.

### Versions

| Version | Shipped with | What changed |
|---|---|---|
| 1.0.0 | Phase 2.9 (`contract-v1`) | The frozen kiosk-facing contract |
| 1.1.0 | Phase 4.0a | The admin console's surface, additive only |

v1.1.0 adds paths, schemas, optional query parameters and response fields, and
removes, renames or narrows nothing — a client generated against v1.0.0 keeps
working, which is the point, because a kiosk in a ward may be a deploy behind.
Two entries deserve naming because they look like changes and are not:

- `LoginRequest.totpCode` moved from required to optional so a recovery code can
  stand in for it. Relaxing a request requirement never breaks a caller that
  still sends the field.
- `AdminRole` gained `admin` / `technician` / `viewer`, the names
  [08](08-admin-console.md) specifies. `superadmin` and `operator` stay in the
  enum until v2 so an older generated client still parses a response; the server
  stops emitting them once 4.1a's migration lands.

The whole admin surface landed in one pass rather than endpoint-by-endpoint so
that no feature task has to reopen the spec mid-flight, and so the kiosk scope
suite classifies every new operation the moment its spec exists — a new endpoint
fails that suite until it is classified, whether or not anyone remembered to
think about it. The handlers behind the not-yet-built endpoints answer 501 with
the task number that fills them in, in
`internal/apiserver/phase4_stubs.go`; that file is empty when Phase 4 is done.
