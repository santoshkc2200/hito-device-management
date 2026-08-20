# Phase 2 Handoff Guide — For Phase 3 (Kiosk) & Phase 4 (Admin Console)

This document provides the definitive integration contract for the frontend teams building the **Phase 3 Kiosk PWA** and **Phase 4 Admin Console**.

---

## 1. Shared Domain Machine & Scenarios

The scan session state machine is defined once in JSON and shared across backend and frontend packages.

| Resource | Path | Description |
|---|---|---|
| **Machine Specification** | `hdms-frontend/packages/domain/session-machine.json` | JSON state machine (states, transitions, timeouts, input classifications). |
| **Scenario Fixtures** | `hdms-frontend/packages/domain/scenarios.json` | Comprehensive test scenarios fixture for frontend XState parity tests. |
| **Domain Package** | `@hdms/domain` (`packages/domain`) | Exposes TypeScript types, machine helpers, and parity assertions. |
| **TypeScript API Client** | `@hdms/api-client` (`packages/api-client`) | Auto-generated strongly typed SDK and Zod schemas from `openapi.yaml`. |

---

## 2. Kiosk Token Scope (Phase 3)

The Kiosk token is a long-lived Bearer token (`Authorization: Bearer <kiosk-token>`).  
Under **FR-45 and INV-11**, the kiosk token has a strictly restricted scope allowlist. It is permitted to call **exactly six operations**:

1. `POST /v1/sessions` — Create a new scan session
2. `GET /v1/sessions/{id}` — Fetch session state (for recovery after reload)
3. `POST /v1/sessions/{id}/scan` — Submit a scanned token (device or user card)
4. `POST /v1/sessions/{id}/return-loan` — Return an item by tapping on screen (manual return)
5. `POST /v1/sessions/{id}/close` — Complete the session (user taps "Done")
6. `DELETE /v1/sessions/{id}` — Cancel/abandon active session

> [!WARNING]
> Any request with a kiosk token to any other endpoint (including user registration, listing users, admin routes, backfill, or device deletion) will be rejected immediately with `403 Forbidden` and audited.

---

## 3. Kiosk Scan Responses & Outcome Handling

Every call to `POST /v1/sessions/{id}/scan` returns a `200 OK` carrying the **complete session state**, the **outcome**, the user's **open loans**, and the **server-authored display message**.

### Outcome Discriminator (`outcome.kind`)

The kiosk UI branches on `outcome.kind`:

| `outcome.kind` | Meaning | Kiosk Display Action |
|---|---|---|
| `device_pending` | Device scanned first, held in session | Prompt: "Device recognized — now scan your ID card" |
| `user_identified` | User card scanned first, identified | Prompt: "Hello Dr. [Name] — scan a device or tap return" |
| `borrowed` | Loan successfully opened | Show success screen with due date; prompt "Scan another or tap Done" |
| `returned` | Loan successfully closed | Show success return message; update open loans list |
| `rejected` | Scan refused (e.g. unknown/unbound card, held by other) | Render server-authored message (`title`, `detail`, `tone`); session auto-resets or continues |
| `duplicate` | Debounced scan within 3 seconds | Ignore silently (no state change) |

### Server-Authored Display Messages

The response includes:
```json
"message": {
  "title": "Borrowed",
  "detail": "Due tomorrow at 5:00 PM",
  "tone": "success"
}
```
`tone` is one of: `"success"` | `"info"` | `"warning"` | `"error"`.  
The kiosk should render `title` and `detail` with the color/icon dictated by `tone`.

---

## 4. Error Handling & RFC 9457 Problem Details

HTTP error responses are formatted as RFC 9457 `application/problem+json`:

```json
{
  "type": "https://hdms.hito.local/errors/session-expired",
  "title": "Session Expired",
  "status": 410,
  "detail": "Scan session has timed out due to inactivity",
  "instance": "/v1/sessions/0192f3c1/scan",
  "requestId": "01JCXYZ..."
}
```

### Key Error Type Suffixes

| Type Suffix | HTTP Status | Context / Action |
|---|---|---|
| `session-expired` | `410 Gone` | Session timed out; kiosk resets to idle screen. |
| `session-conflict` | `409 Conflict` | Concurrent scan detected on same session; reload session state. |
| `device-on-loan` | `409 Conflict` | Device already out (`extensions` contains `holderDepartment`, never personal name). |
| `overlapping-custody` | `409 Conflict` | Backfill conflict on temporal custody (INV-13). |
| `backdated-not-permitted` | `422 Unprocessable` | Non-backfill live endpoint received a past timestamp. |
| `device-unavailable` | `409 Conflict` | Device in maintenance / retired / lost. |
| `user-suspended` | `403 Forbidden` | Borrower account suspended. |
| `idempotency-mismatch` | `422 Unprocessable` | Same `Idempotency-Key` sent with a different body payload. |
| `invalid-token-format` | `400 Bad Request` | Scanned barcode payload malformed. |
| `rate-limited` | `429 Too Many Requests` | Rate limit exceeded; check `Retry-After` header. |

---

## 5. Kiosk Device Pairing Flow (iPad Initial Setup)

For pairing a physical iPad kiosk running the PWA:

1. **Admin generates pairing code:**
   - Admin in Admin Console calls: `POST /v1/kiosks/{id}/pairing-code`
   - Returns: `{ "code": "839201", "expiresAt": "2026-08-20T10:15:00Z" }` (valid for 15 minutes).
2. **Kiosk redeems code:**
   - iPad displays pairing setup screen. Attendant enters 6-digit code.
   - iPad sends unauthenticated: `POST /v1/kiosks/pair` with `{ "code": "839201" }`.
   - Returns: `{ "kioskId": "...", "name": "Ward 3 Kiosk", "token": "..." }`.
3. **Storage:**
   - Kiosk stores token in secure local storage and uses `Authorization: Bearer <token>` for all future session requests.

---

## 6. Real-Time Events via Server-Sent Events (Admin Console)

Admin Console subscribes to live loan and device updates via:
- `GET /v1/events/stream`
- Receives SSE frames (`loan.opened`, `loan.closed`, `device.status_changed`).
- Reconnects automatically using the standard `Last-Event-ID` header to replay missed outbox events without loss.
