# Kiosk Expected Return Date Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every kiosk loan gets an expected return date the borrower can choose, and no kiosk loan can run into a live reservation (including the booking gap).

**Architecture:** The loan still opens on the device scan, with a default due date clamped by a return window that the reservations module computes (`next active reservation start − ReturnBuffer`, `borrow + MaxDurationDays`). A reservation too close to leave 30 usable minutes counts as "in force", so the existing machine refusal/collect rules handle it with no table change. A new kiosk endpoint changes the due date after the borrow, re-validating the window under the same device advisory lock that loan creation and staff booking use. The kiosk success screen gains a "Return by" panel.

**Tech Stack:** Go 1.26, pgx v5, sqlc, oapi-codegen, PostgreSQL; React 19 + XState v5 + Vitest (kiosk), hey-api generated client, Playwright e2e.

**Spec:** `docs/superpowers/specs/2026-09-29-kiosk-return-date-design.md`

## Global Constraints

- Maximum loan length: `settings.BookingPolicy.MaxDurationDays` (default 30 days); gap: `settings.BookingPolicy.ReturnBufferMinutes` (default 60 minutes, 0 allowed); pre-window: `settings.Policy.ReservationPreWindowMinutes` (default 30 minutes, `<= 0` falls back to 30).
- Settings read failure never fails a scan: fall back to the defaults above.
- Minimum usable walk-up loan before the next reservation: `MinUsableLoan = 30 * time.Minute` (constant, not a setting).
- Default due date when the category has no `default_loan_period`: borrow time + 24 hours.
- Only reservations with status `active` limit a new loan.
- New endpoint: `POST /v1/sessions/{id}/loan-due-date`, kiosk token, Idempotency-Key; body `{loanId, dueAt}`; 200 body `{loanId, dueAt, latestReturnAt?, sessionExpiresAt}`.
- Problems: `409 due-date-conflict` with extension `latestReturnAt`; `422 validation-failed` (field `dueAt`) when `dueAt` is not in the future; session errors as `return-loan`.
- Kiosk chips: Today 17:00 (hidden once past), Tomorrow 17:00, +3 days 17:00, +1 week 17:00, Other…; chips after `latestReturnAt` disabled; "Latest: <time>" chip when every chip is disabled. Custom picker: 15-minute steps.
- Kiosk success auto-dismiss: 4 s for returns, 12 s for borrow and collected, reset on every panel touch.
- User-visible kiosk strings live in `apps/kiosk/src/i18n/en.ts` and `ja.ts` (the `no-literals` test enforces this); en and ja must have the same keys.
- Backend responses stay code-only; the kiosk renders copy.
- Code comments, commit messages and docs are plain English prose matching the surrounding style.

## Review Focus

- Borrower picks a return exactly equal to `latestReturnAt` → accepted (the bound is inclusive). Test in Task 4.
- A staff booking lands between the kiosk borrow and the date change → 409 `due-date-conflict`, the panel moves the selection to the new latest and says why. Tests in Tasks 4 and 8.
- Borrow after 17:00 local time → no "Today" chip, and chips are computed in the kiosk's local time while the API carries ISO instants. Test in Task 6.
- Borrower keeps tapping for longer than the 25-second `ready` timeout → the session does not expire under them (each successful change re-enters `ready`, and the server refreshes `expires_at`). Tests in Tasks 4 and 7.
- A borrower who changed a date keeps it for the next device, but a return window smaller than their preference clamps it rather than refusing. Test in Task 3.

---

## File Structure

Backend (`hdms-backend/`):
- `internal/platform/db/lock.go` (create): `LockDevice`, the one place the device advisory-lock SQL lives.
- `internal/modules/lending/module.go`, `internal/apiserver/staff_reservations.go` (modify): use `db.LockDevice`.
- `queries/reservations/reservations.sql` (modify): `NextActiveReservationStart`.
- `internal/modules/reservations/module.go` (modify): `NextActiveStartForDevice`.
- `internal/modules/reservations/checkoutadapter.go` (modify): policy read, effective lead, `ReturnWindowFor`.
- `internal/modules/reservations/checkoutadapter_test.go` (create): pure window/lead tests.
- `internal/modules/checkout/deps.go` (modify): `ReturnWindow`, `ReservationLookup.ReturnWindowFor`, `Loans.SetDueAt`.
- `internal/modules/checkout/duedate.go` (create): `chooseDueAt`, `returnWindow` helper.
- `internal/modules/checkout/duedate_test.go` (create).
- `internal/modules/checkout/execute.go` (modify): lock + clamp in `executeBorrow`.
- `internal/modules/checkout/setduedate.go` (create): `SetLoanDueDate`.
- `internal/modules/checkout/checkoutapi/checkout.go` (modify): `ScanParams.PreferredDueAt`, `Outcome.LatestReturnAt`, `LoanDueDate`, errors, interface method.
- `queries/lending/lending.sql`, `internal/modules/lending/module.go`, `internal/modules/lending/lendingapi/lending.go` (modify): `SetDueAt`.
- `api/openapi.yaml` (modify) + regenerated `internal/platform/httpx/gen/api.gen.go`.
- `internal/apiserver/sessions.go`, `internal/apiserver/helpers.go` (modify): handler, scan field, error mapping.
- `internal/platform/auth/kioskscope.go`, `internal/platform/auth/roles.go`, `internal/platform/httpx/metrics.go` (modify): classify the new route.
- `test/integration/kiosk_return_window_test.go` (create): service-level integration tests for Tasks 2–4.
- `test/integration/sessions_http_test.go` (modify): extend the session workflow test for Task 5.

Frontend (`hdms-frontend/`):
- `packages/api-client/src/gen/*` (regenerated).
- `apps/kiosk/src/lib/return-date-options.ts` + `.test.ts` (create): chip and slot math.
- `apps/kiosk/src/machine/session-machine.ts` (modify): `preferredDueAt`, `LOAN_DUE_UPDATED`, `executeSetDueDate`.
- `apps/kiosk/src/machine/use-kiosk-session.ts` (modify): pass `preferredDueAt`, sound key by loan id, expose `applyDueUpdate`.
- `apps/kiosk/src/machine/return-date.test.ts` (create).
- `apps/kiosk/src/components/return-by-panel.tsx` + `.test.tsx` (create).
- `apps/kiosk/src/screens/success-screen.tsx` + `.test.tsx` (modify).
- `apps/kiosk/src/routes/index.tsx` (modify): wire panel props.
- `apps/kiosk/src/i18n/en.ts`, `ja.ts` (modify).
- `e2e/helpers/test-api.ts`, `e2e/kiosk.spec.ts`, `e2e/README.md` (modify).

Docs: `docs/04-scanning-and-checkout-flows.md`, `docs/07-kiosk-app.md`, `docs/phases/phase-6/6.4-reservations.md`.

Commands used throughout (run from the named directory):
- Backend unit: `cd hdms-backend && go test ./internal/...`
- Backend integration (needs Docker): `cd hdms-backend && go test -race -tags=integration ./test/integration/ -run <Name>`
- sqlc: `cd hdms-backend && sqlc generate`
- OpenAPI: `task generate:backend && task generate:frontend` (repo root)
- Lint: `task lint:backend`, `task lint:frontend`
- Kiosk: `cd hdms-frontend && pnpm --filter kiosk test -- <file>` and `pnpm -w build`

---

### Task 1: Shared device lock and reservation return window

**Files:**
- Create: `hdms-backend/internal/platform/db/lock.go`
- Modify: `hdms-backend/internal/modules/lending/module.go:88-92`
- Modify: `hdms-backend/internal/apiserver/staff_reservations.go:60-64`
- Modify: `hdms-backend/queries/reservations/reservations.sql` (append)
- Modify: `hdms-backend/internal/modules/reservations/module.go` (add method after `ActiveOrUpcomingForDevice`)
- Modify: `hdms-backend/internal/modules/reservations/checkoutadapter.go`
- Modify: `hdms-backend/internal/modules/checkout/deps.go:63-92`
- Create: `hdms-backend/internal/modules/reservations/checkoutadapter_test.go`
- Test: `hdms-backend/test/integration/kiosk_return_window_test.go`

**Interfaces:**
- Produces: `db.LockDevice(ctx context.Context, pool *db.Pool, deviceID string) error`
- Produces: `checkout.ReturnWindow{Latest time.Time; CollectedEndAt time.Time}`
- Produces: `checkout.ReservationLookup.ReturnWindowFor(ctx, deviceID string, from time.Time, collectingID string) (checkout.ReturnWindow, error)`
- Produces: `(*reservations.Service).NextActiveStartForDevice(ctx, deviceID string, from time.Time, excludeID string) (*time.Time, error)`
- Produces: `reservations.MinUsableLoan`, `reservations.DefaultReturnBuffer`, `reservations.DefaultMaxLoan`
- Behaviour change: `CheckoutAdapter.InForceFor` treats a reservation starting within `max(preWindow, buffer + MinUsableLoan)` as in force.

- [ ] **Step 1: Write the failing pure tests**

Create `hdms-backend/internal/modules/reservations/checkoutadapter_test.go`:

```go
package reservations

import (
	"testing"
	"time"
)

func TestPolicyLeadIsTheLongerOfPreWindowAndBufferPlusMinimumLoan(t *testing.T) {
	cases := []struct {
		name string
		p    policy
		want time.Duration
	}{
		{"buffer dominates", policy{preWindow: 30 * time.Minute, buffer: 60 * time.Minute}, 90 * time.Minute},
		{"pre-window dominates", policy{preWindow: 3 * time.Hour, buffer: 60 * time.Minute}, 3 * time.Hour},
		{"zero buffer still keeps the minimum loan", policy{preWindow: 10 * time.Minute, buffer: 0}, 30 * time.Minute},
	}
	for _, tc := range cases {
		if got := tc.p.lead(); got != tc.want {
			t.Errorf("%s: lead() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestLatestReturn(t *testing.T) {
	from := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	p := policy{buffer: 60 * time.Minute, maxLoan: 30 * 24 * time.Hour}

	if got, want := latestReturn(from, nil, p), from.Add(30*24*time.Hour); !got.Equal(want) {
		t.Errorf("no next reservation: latest = %v, want %v", got, want)
	}
	next := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	if got, want := latestReturn(from, &next, p), next.Add(-time.Hour); !got.Equal(want) {
		t.Errorf("next reservation tomorrow 10:00: latest = %v, want %v", got, want)
	}
	far := from.Add(60 * 24 * time.Hour)
	if got, want := latestReturn(from, &far, p), from.Add(30*24*time.Hour); !got.Equal(want) {
		t.Errorf("next reservation beyond max: latest = %v, want %v", got, want)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd hdms-backend && go test ./internal/modules/reservations/ -run 'TestPolicyLead|TestLatestReturn'`
Expected: FAIL to compile — `undefined: policy`, `undefined: latestReturn`.

- [ ] **Step 3: Add the shared lock helper and use it in both existing callers**

Create `hdms-backend/internal/platform/db/lock.go`:

```go
package db

import "context"

// LockDevice takes the transaction-scoped advisory lock that serializes every
// write deciding a device's custody window: opening a loan, changing its
// expected return, and a staff booking. It must run inside a transaction
// (db.NewTxManager(...).Do); outside one the lock is released immediately.
// The lock is re-entrant within a transaction.
func LockDevice(ctx context.Context, pool *Pool, deviceID string) error {
	_, err := Conn(ctx, pool).Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))`, deviceID)
	return err
}
```

In `hdms-backend/internal/modules/lending/module.go` inside `OpenLoan`'s transaction, replace

```go
		if _, err := db.Conn(ctx, s.pool).Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))`, did); err != nil {
			return err
		}
```

with

```go
		if err := db.LockDevice(ctx, s.pool, deviceID); err != nil {
			return err
		}
```

(`deviceID` is `OpenLoan`'s string parameter; keep the comment above it.)

In `hdms-backend/internal/apiserver/staff_reservations.go` replace

```go
		if _, err := db.Conn(ctx, s.pool).Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))`, body.DeviceId); err != nil {
			return err
		}
```

with

```go
		if err := db.LockDevice(ctx, s.pool, deviceID); err != nil {
			return err
		}
```

(`deviceID := body.DeviceId.String()` is already declared just above the transaction.) Both old calls cast a UUID to text; the new calls pass the canonical lowercase string, which hashes identically.

- [ ] **Step 4: Add the next-reservation query and service method**

Append to `hdms-backend/queries/reservations/reservations.sql`:

```sql
-- name: NextActiveReservationStart :one
-- The earliest reservation that will still need the device after from_at.
-- Only 'active' counts: a collected reservation is already in its reserver's
-- hands. exclude_id skips the reservation a borrow is collecting.
SELECT start_at
FROM reservations
WHERE device_id = @device_id
  AND status = 'active'
  AND end_at > @from_at
  AND (sqlc.narg('exclude_id')::uuid IS NULL OR id <> sqlc.narg('exclude_id')::uuid)
ORDER BY start_at ASC
LIMIT 1;
```

Run: `cd hdms-backend && sqlc generate`
Expected: `internal/modules/reservations/internal/store/reservations.sql.go` gains `NextActiveReservationStart(ctx, arg NextActiveReservationStartParams) (pgtype.Timestamptz, error)`. Open the generated file and use the exact field names it chose for `device_id`, `from_at`, `exclude_id` in the next snippet (expected `DeviceID`, `FromAt`, `ExcludeID`).

Add to `hdms-backend/internal/modules/reservations/module.go` after `ActiveOrUpcomingForDevice`:

```go
// NextActiveStartForDevice returns when the earliest active reservation on
// deviceID that has not ended by `from` starts, ignoring excludeID ("" for
// none). nil means no reservation limits a loan opened now. It reads through
// the caller's transaction, if any, so a checkout holding the device lock
// sees exactly what a concurrent staff booking committed.
func (s *Service) NextActiveStartForDevice(ctx context.Context, deviceID string, from time.Time, excludeID string) (*time.Time, error) {
	did, err := pgtypeconv.UUID(deviceID)
	if err != nil {
		return nil, fmt.Errorf("reservations: invalid device id: %w", err)
	}
	exclude, err := pgtypeconv.NullUUID(excludeID)
	if err != nil {
		return nil, fmt.Errorf("reservations: invalid reservation id: %w", err)
	}
	q := reservationsstore.New(db.Conn(ctx, s.pool))
	start, err := q.NextActiveReservationStart(ctx, reservationsstore.NextActiveReservationStartParams{
		DeviceID:  did,
		FromAt:    pgtypeconv.Timestamptz(from),
		ExcludeID: exclude,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("reservations: next active reservation: %w", err)
	}
	t := pgtypeconv.Time(start)
	return &t, nil
}
```

Add the `db` import (`github.com/hito-hospital/hdms/internal/platform/db`) if the file does not already have it.

- [ ] **Step 5: Add `ReturnWindow` to checkout's dependency contract**

In `hdms-backend/internal/modules/checkout/deps.go`, add to the `ReservationLookup` interface (after `MarkCollected`):

```go
	// ReturnWindowFor reports the return window for a loan of deviceID
	// opened at `from`. collectingID names the reservation this borrow
	// collects ("" for a walk-up borrow): it is skipped when looking for
	// the next reservation, and its end is reported so the borrow can
	// default to it.
	ReturnWindowFor(ctx context.Context, deviceID string, from time.Time, collectingID string) (ReturnWindow, error)
```

and after `ReservationInForce`:

```go
// ReturnWindow bounds a loan's expected return. Latest is the latest
// allowed return: the maximum loan length, or the booking gap before the
// device's next reservation, whichever comes first. A zero Latest means no
// bound (no reservations module wired). CollectedEndAt is the end of the
// reservation being collected, zero otherwise.
type ReturnWindow struct {
	Latest         time.Time
	CollectedEndAt time.Time
}
```

- [ ] **Step 6: Rewrite the adapter's policy read, lead and window**

In `hdms-backend/internal/modules/reservations/checkoutadapter.go`, replace the `DefaultPreWindow` block and the `preWindow` method with:

```go
// Defaults used when settings cannot be read. A scan must never fail
// because a policy knob was unreadable; these are the documented defaults
// the settings columns themselves carry.
const (
	DefaultPreWindow    = 30 * time.Minute
	DefaultReturnBuffer = 60 * time.Minute
	DefaultMaxLoan      = 30 * 24 * time.Hour
)

// MinUsableLoan is the shortest walk-up loan worth opening before the
// device's next reservation. When the booking gap leaves less than this,
// the reservation is treated as already in force, so the kiosk refuses the
// walk-up (or lets the reserver collect early) instead of opening a loan
// that must come back within minutes.
const MinUsableLoan = 30 * time.Minute

// policy is the slice of settings the checkout boundary needs.
type policy struct {
	preWindow time.Duration
	buffer    time.Duration
	maxLoan   time.Duration
}

// lead is how long before a reservation's start the device stops being
// walk-up borrowable.
func (p policy) lead() time.Duration {
	return max(p.preWindow, p.buffer+MinUsableLoan)
}

// latestReturn is the latest expected return for a loan opened at from,
// given the start of the next reservation that still needs the device.
func latestReturn(from time.Time, next *time.Time, p policy) time.Time {
	latest := from.Add(p.maxLoan)
	if next != nil {
		if bound := next.Add(-p.buffer); bound.Before(latest) {
			latest = bound
		}
	}
	return latest
}

func (a *CheckoutAdapter) policy(ctx context.Context) policy {
	p := policy{preWindow: DefaultPreWindow, buffer: DefaultReturnBuffer, maxLoan: DefaultMaxLoan}
	if a.settings == nil {
		return p
	}
	st, err := a.settings.GetSettings(ctx)
	if err != nil {
		return p
	}
	if st.Policy.ReservationPreWindowMinutes > 0 {
		p.preWindow = time.Duration(st.Policy.ReservationPreWindowMinutes) * time.Minute
	}
	if st.BookingPolicy.ReturnBufferMinutes >= 0 {
		p.buffer = time.Duration(st.BookingPolicy.ReturnBufferMinutes) * time.Minute
	}
	if st.BookingPolicy.MaxDurationDays > 0 {
		p.maxLoan = time.Duration(st.BookingPolicy.MaxDurationDays) * 24 * time.Hour
	}
	return p
}
```

Update the struct's doc comment to mention the return window as well as the pre-window. In `InForceFor`, replace `a.preWindow(ctx)` with `a.policy(ctx).lead()` and add one sentence to its comment: "A reservation too close to leave MinUsableLoan after the booking gap is in force too." Add:

```go
// ReturnWindowFor reports how long a loan of deviceID opened at from may
// run (checkout.ReservationLookup).
func (a *CheckoutAdapter) ReturnWindowFor(ctx context.Context, deviceID string, from time.Time, collectingID string) (checkout.ReturnWindow, error) {
	var w checkout.ReturnWindow
	if collectingID != "" {
		res, err := a.svc.GetReservation(ctx, collectingID)
		if err != nil {
			return checkout.ReturnWindow{}, fmt.Errorf("reservations: reservation being collected: %w", err)
		}
		w.CollectedEndAt = res.EndAt
	}
	next, err := a.svc.NextActiveStartForDevice(ctx, deviceID, from, collectingID)
	if err != nil {
		return checkout.ReturnWindow{}, fmt.Errorf("reservations: return window: %w", err)
	}
	w.Latest = latestReturn(from, next, a.policy(ctx))
	return w, nil
}
```

- [ ] **Step 7: Run pure tests and build**

Run: `cd hdms-backend && go test ./internal/modules/reservations/ && go build ./...`
Expected: PASS, build succeeds.

- [ ] **Step 8: Write the integration test for adapter behaviour against Postgres**

Create `hdms-backend/test/integration/kiosk_return_window_test.go`:

```go
//go:build integration

package integration

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/audit"
	"github.com/hito-hospital/hdms/internal/modules/catalog"
	"github.com/hito-hospital/hdms/internal/modules/checkout"
	"github.com/hito-hospital/hdms/internal/modules/credentials"
	"github.com/hito-hospital/hdms/internal/modules/identity"
	"github.com/hito-hospital/hdms/internal/modules/lending"
	"github.com/hito-hospital/hdms/internal/modules/reservations"
	"github.com/hito-hospital/hdms/internal/modules/reservations/reservationsapi"
	"github.com/hito-hospital/hdms/internal/platform/clock"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/events"
	"github.com/hito-hospital/hdms/internal/platform/settings"
	"github.com/hito-hospital/hdms/test/fixtures"
	"github.com/hito-hospital/hdms/test/testdb"
)

// returnWindowEnv wires checkout with the real reservations adapter and
// settings, which newScanCheckoutService leaves out.
type returnWindowEnv struct {
	pool     *db.Pool
	checkout *checkout.Service
	res      *reservations.Service
	adapter  *reservations.CheckoutAdapter
	lending  *lending.Service
}

func newReturnWindowEnv(t *testing.T, c clock.Clock) returnWindowEnv {
	t.Helper()
	pool := testdb.New(t)
	auditSvc := audit.New(pool)
	lendingSvc := lending.New(pool, auditSvc, c)
	settingsSvc := settings.New(pool, auditSvc)
	resSvc := reservations.New(pool, auditSvc, c)
	adapter := reservations.NewCheckoutAdapter(resSvc, settingsSvc)
	key := make([]byte, 32)
	credentialsSvc := credentials.New(pool, auditSvc, "fixtures-test-pepper", key)
	deps := checkout.Deps{
		Users: identity.New(pool, auditSvc), Devices: catalog.New(pool, auditSvc), Tokens: credentialsSvc,
		Loans: lendingSvc, Settings: settingsSvc, Reservations: adapter,
	}
	svc := checkout.New(pool, c, deps, auditSvc, events.NewBus(slog.New(slog.DiscardHandler)))
	return returnWindowEnv{pool: pool, checkout: svc, res: resSvc, adapter: adapter, lending: lendingSvc}
}

func (e returnWindowEnv) reserve(t *testing.T, deviceID, userID string, start, end time.Time) reservationsapi.Reservation {
	t.Helper()
	r, err := e.res.CreateReservation(context.Background(), reservationsapi.CreateParams{
		DeviceID: deviceID, UserID: userID, StartAt: start, EndAt: end,
		CreatedBy: "admin:test", CreatedSource: "admin",
	})
	if err != nil {
		t.Fatalf("CreateReservation: %v", err)
	}
	return r
}

func TestReturnWindowStopsTheBufferBeforeTheNextReservation(t *testing.T) {
	env := newReturnWindowEnv(t, clock.System{})
	ctx := context.Background()
	deviceID := fixtures.AvailableDevice(t, env.pool)
	userID := fixtures.User(t, env.pool)
	now := time.Now().UTC().Truncate(time.Second)
	start := now.Add(26 * time.Hour)
	env.reserve(t, deviceID, userID, start, start.Add(2*time.Hour))

	w, err := env.adapter.ReturnWindowFor(ctx, deviceID, now, "")
	if err != nil {
		t.Fatalf("ReturnWindowFor: %v", err)
	}
	if want := start.Add(-60 * time.Minute); !w.Latest.Equal(want) {
		t.Fatalf("Latest = %v, want %v (next start minus the 60-minute default gap)", w.Latest, want)
	}
	if !w.CollectedEndAt.IsZero() {
		t.Fatalf("CollectedEndAt = %v, want zero for a walk-up borrow", w.CollectedEndAt)
	}
}

func TestReturnWindowSkipsTheReservationBeingCollected(t *testing.T) {
	env := newReturnWindowEnv(t, clock.System{})
	ctx := context.Background()
	deviceID := fixtures.AvailableDevice(t, env.pool)
	userID := fixtures.User(t, env.pool)
	now := time.Now().UTC().Truncate(time.Second)
	mine := env.reserve(t, deviceID, userID, now.Add(10*time.Minute), now.Add(4*time.Hour))
	nextStart := now.Add(30 * time.Hour)
	env.reserve(t, deviceID, fixtures.User(t, env.pool), nextStart, nextStart.Add(time.Hour))

	w, err := env.adapter.ReturnWindowFor(ctx, deviceID, now, mine.ID)
	if err != nil {
		t.Fatalf("ReturnWindowFor: %v", err)
	}
	if !w.CollectedEndAt.Equal(mine.EndAt) {
		t.Fatalf("CollectedEndAt = %v, want %v", w.CollectedEndAt, mine.EndAt)
	}
	if want := nextStart.Add(-60 * time.Minute); !w.Latest.Equal(want) {
		t.Fatalf("Latest = %v, want %v (the other reservation, not the collected one)", w.Latest, want)
	}
}

func TestReturnWindowWithoutReservationsIsTheMaximumLoan(t *testing.T) {
	env := newReturnWindowEnv(t, clock.System{})
	deviceID := fixtures.AvailableDevice(t, env.pool)
	now := time.Now().UTC().Truncate(time.Second)
	w, err := env.adapter.ReturnWindowFor(context.Background(), deviceID, now, "")
	if err != nil {
		t.Fatalf("ReturnWindowFor: %v", err)
	}
	if want := now.Add(30 * 24 * time.Hour); !w.Latest.Equal(want) {
		t.Fatalf("Latest = %v, want %v", w.Latest, want)
	}
}

func TestReservationInsideBufferPlusMinimumLoanIsInForce(t *testing.T) {
	env := newReturnWindowEnv(t, clock.System{})
	ctx := context.Background()
	deviceID := fixtures.AvailableDevice(t, env.pool)
	userID := fixtures.User(t, env.pool)
	now := time.Now().UTC()
	// Default pre-window 30 min, gap 60 min: the effective lead is 90 min.
	// 75 minutes ahead is outside the pre-window but inside the lead.
	near := env.reserve(t, deviceID, userID, now.Add(75*time.Minute), now.Add(3*time.Hour))
	res, ok, err := env.adapter.InForceFor(ctx, deviceID, now)
	if err != nil || !ok || res.ID != near.ID {
		t.Fatalf("InForceFor = (%+v, %v, %v), want reservation %s in force", res, ok, err, near.ID)
	}

	other := fixtures.AvailableDevice(t, env.pool)
	env.reserve(t, other, userID, now.Add(100*time.Minute), now.Add(3*time.Hour))
	if _, ok, err := env.adapter.InForceFor(ctx, other, now); err != nil || ok {
		t.Fatalf("InForceFor 100 minutes ahead = (%v, %v), want not in force", ok, err)
	}
}
```

- [ ] **Step 9: Run the integration tests**

Run: `cd hdms-backend && go test -race -tags=integration ./test/integration/ -run 'TestReturnWindow|TestReservationInsideBuffer'`
Expected: PASS (4 tests). Also run the staff booking and lending suites touched by the lock refactor: `go test -race -tags=integration ./test/integration/ -run 'TestStaff.*Reserv|Concurren|Lending'` — Expected: PASS.

- [ ] **Step 10: Commit**

```bash
git add hdms-backend/internal/platform/db/lock.go hdms-backend/internal/modules/lending/module.go hdms-backend/internal/apiserver/staff_reservations.go hdms-backend/queries/reservations/reservations.sql hdms-backend/internal/modules/reservations hdms-backend/internal/modules/checkout/deps.go hdms-backend/test/integration/kiosk_return_window_test.go
git commit -m "feat(reservations): compute the return window for kiosk loans

The checkout adapter now reports the latest expected return a loan may
have (the booking gap before the next active reservation, or the maximum
loan length) and treats a reservation too close to leave 30 usable
minutes as already in force. The device advisory lock moves into
db.LockDevice so loan creation and staff booking share one definition."
```

---

### Task 2: Borrow opens the loan with a clamped default due date

**Files:**
- Create: `hdms-backend/internal/modules/checkout/duedate.go`
- Create: `hdms-backend/internal/modules/checkout/duedate_test.go`
- Modify: `hdms-backend/internal/modules/checkout/checkoutapi/checkout.go:126-136,173-179`
- Modify: `hdms-backend/internal/modules/checkout/execute.go:113-199`
- Test: `hdms-backend/test/integration/kiosk_return_window_test.go`

**Interfaces:**
- Consumes: `checkout.ReturnWindow`, `ReservationLookup.ReturnWindowFor`, `db.LockDevice` (Task 1)
- Produces: `checkoutapi.ScanParams.PreferredDueAt *time.Time`; `checkoutapi.Outcome.LatestReturnAt *time.Time`
- Produces: `checkout.DefaultLoanWithoutPeriod = 24 * time.Hour`; `func chooseDueAt(now time.Time, w ReturnWindow, preferred, categoryDefault *time.Time) time.Time`; `func (s *Service) returnWindow(ctx context.Context, deviceID, collectingID string, from time.Time) (ReturnWindow, error)`

- [ ] **Step 1: Write the failing unit tests**

Create `hdms-backend/internal/modules/checkout/duedate_test.go`:

```go
package checkout

import (
	"testing"
	"time"
)

func TestChooseDueAt(t *testing.T) {
	now := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	at := func(h int) *time.Time { v := now.Add(time.Duration(h) * time.Hour); return &v }
	latest := now.Add(20 * time.Hour)

	cases := []struct {
		name      string
		w         ReturnWindow
		preferred *time.Time
		category  *time.Time
		want      time.Time
	}{
		{"collected reservation end wins", ReturnWindow{Latest: latest, CollectedEndAt: *at(6)}, at(3), at(48), *at(6)},
		{"preferred beats category", ReturnWindow{Latest: latest}, at(3), at(8), *at(3)},
		{"past preferred is ignored", ReturnWindow{Latest: latest}, at(-1), at(8), *at(8)},
		{"category default", ReturnWindow{Latest: latest}, nil, at(8), *at(8)},
		{"no category period gives 24 hours, clamped", ReturnWindow{Latest: latest}, nil, nil, latest},
		{"no category period, no cap", ReturnWindow{}, nil, nil, now.Add(24 * time.Hour)},
		{"preferred clamped to latest", ReturnWindow{Latest: latest}, at(72), nil, latest},
		{"latest already past never yields a past due date", ReturnWindow{Latest: now.Add(-time.Minute)}, nil, at(8), now},
	}
	for _, tc := range cases {
		if got := chooseDueAt(now, tc.w, tc.preferred, tc.category); !got.Equal(tc.want) {
			t.Errorf("%s: chooseDueAt = %v, want %v", tc.name, got, tc.want)
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd hdms-backend && go test ./internal/modules/checkout/ -run TestChooseDueAt`
Expected: FAIL to compile — `undefined: chooseDueAt`.

- [ ] **Step 3: Implement `duedate.go`**

Create `hdms-backend/internal/modules/checkout/duedate.go`:

```go
package checkout

import (
	"context"
	"fmt"
	"time"
)

// DefaultLoanWithoutPeriod is the expected return offered when the device's
// category sets no loan period. Every kiosk loan carries a due date, so the
// borrower always has something to confirm or change, and staff booking can
// see when the device comes back.
const DefaultLoanWithoutPeriod = 24 * time.Hour

// chooseDueAt picks a new loan's expected return. The first of these wins:
// the end of the reservation being collected, the borrower's preferred
// return (their last choice in this session, if still in the future), the
// category's default period, or 24 hours. The result is then clamped to the
// return window's Latest (zero means unbounded) and never falls before now.
func chooseDueAt(now time.Time, w ReturnWindow, preferred, categoryDefault *time.Time) time.Time {
	var due time.Time
	switch {
	case !w.CollectedEndAt.IsZero():
		due = w.CollectedEndAt
	case preferred != nil && preferred.After(now):
		due = *preferred
	case categoryDefault != nil:
		due = *categoryDefault
	default:
		due = now.Add(DefaultLoanWithoutPeriod)
	}
	if !w.Latest.IsZero() && due.After(w.Latest) {
		due = w.Latest
	}
	if due.Before(now) {
		due = now
	}
	return due
}

// returnWindow asks the reservations module how long a loan of deviceID
// opened at from may run. A nil Reservations dep (deployments and tests
// predating 6.4) means no bound.
func (s *Service) returnWindow(ctx context.Context, deviceID, collectingID string, from time.Time) (ReturnWindow, error) {
	if s.deps.Reservations == nil {
		return ReturnWindow{}, nil
	}
	w, err := s.deps.Reservations.ReturnWindowFor(ctx, deviceID, from, collectingID)
	if err != nil {
		return ReturnWindow{}, fmt.Errorf("checkout: return window: %w", err)
	}
	return w, nil
}
```

- [ ] **Step 4: Run the unit test**

Run: `cd hdms-backend && go test ./internal/modules/checkout/ -run TestChooseDueAt`
Expected: PASS.

- [ ] **Step 5: Extend the checkout API types**

In `hdms-backend/internal/modules/checkout/checkoutapi/checkout.go`:

`Outcome` — after `DueAt  *time.Time` add:

```go
	// LatestReturnAt is the latest expected return the borrower may choose
	// for this loan (borrow and reservation_collected only; nil when no
	// bound applies).
	LatestReturnAt *time.Time
```

`ScanParams` — after `Actor     string` add:

```go
	// PreferredDueAt is the expected return the borrower chose for their
	// previous device in this session. A borrow uses it as the default when
	// it is still in the future, clamped to the device's return window.
	PreferredDueAt *time.Time
```

- [ ] **Step 6: Wire the lock, window and clamp into `executeBorrow`**

In `hdms-backend/internal/modules/checkout/execute.go`, `executeBorrow`:

Directly before `conn := db.Conn(ctx, s.pool)` insert:

```go
	// Hold the device lock before reading the return window, so a staff
	// booking cannot commit between the window check and the loan insert.
	// Taken outside the savepoint so a lost borrow race keeps it.
	if err := db.LockDevice(ctx, s.pool, deviceID); err != nil {
		return checkoutapi.Outcome{}, checkoutapi.Message{}, checkoutstore.ScanSession{}, fmt.Errorf("checkout: lock device to borrow: %w", err)
	}
```

Replace the block from `var dueAt *time.Time` through the closing `}` of `if device.CategoryID != "" { ... }` with:

```go
	now := s.clock.Now()
	var categoryDue *time.Time
	if device.CategoryID != "" {
		cat, err := s.deps.Devices.CategoryOf(ctx, device.CategoryID)
		if err != nil {
			return checkoutapi.Outcome{}, checkoutapi.Message{}, checkoutstore.ScanSession{}, fmt.Errorf("checkout: category of device to borrow: %w", err)
		}
		categoryDue = s.deps.Loans.DueDateFor(cat.DefaultLoanPeriod, now)
	}
	window, err := s.returnWindow(ctx, deviceID, decision.FulfillsReservationID, now)
	if err != nil {
		return checkoutapi.Outcome{}, checkoutapi.Message{}, checkoutstore.ScanSession{}, err
	}
	due := chooseDueAt(now, window, params.PreferredDueAt, categoryDue)
```

and change the `OpenLoan` call's third argument from `dueAt` to `&due`.

Replace the outcome literal with:

```go
	outcome := checkoutapi.Outcome{
		Kind: kind, LoanID: loan.ID,
		Device: &checkoutapi.DeviceView{ID: device.ID, AssetTag: device.AssetTag, Name: device.Name}, DueAt: loan.DueAt,
	}
	if !window.Latest.IsZero() {
		outcome.LatestReturnAt = &window.Latest
	}
```

Note: the overdue-block branch earlier in the function already declares a `now` inside its loop scope; the new `now` sits at function scope after it, so there is no clash. If the compiler reports a redeclaration, rename the inner one.

- [ ] **Step 7: Build and run the existing checkout suite**

Run: `cd hdms-backend && go build ./... && go test -race -tags=integration ./test/integration/ -run 'TestScenario|TestReturnLoan|Concurren|Matrix|Invariant'`
Expected: PASS. Fixture categories have no loan period, so those loans now get a 24-hour due date instead of none; if any existing test asserted a nil `DueAt` for a kiosk borrow, update it to assert a due date 24 hours after the borrow and mention it in the commit message.

- [ ] **Step 8: Add integration tests for the borrow**

Append to `hdms-backend/test/integration/kiosk_return_window_test.go` (add `checkoutapi` and `credentialsapi` imports: `github.com/hito-hospital/hdms/internal/modules/checkout/checkoutapi`, `github.com/hito-hospital/hdms/internal/modules/credentials/credentialsapi`):

```go
// borrowIn scans the user then the device in a fresh session and returns
// the borrow result, with preferred sent on the device scan.
func (e returnWindowEnv) borrowIn(t *testing.T, kioskID, userToken, deviceToken string, preferred *time.Time) (checkoutapi.Session, checkoutapi.ScanResult) {
	t.Helper()
	ctx := context.Background()
	session := createSession(t, ctx, e.checkout, kioskID)
	scan(t, ctx, e.checkout, session.ID, userToken)
	r, err := e.checkout.Scan(ctx, checkoutapi.ScanParams{
		SessionID: session.ID, Token: deviceToken, Source: "scanner", Actor: "kiosk:test", PreferredDueAt: preferred,
	})
	if err != nil {
		t.Fatalf("Scan device: %v", err)
	}
	return session, r
}

func TestBorrowClampsTheDefaultBeforeTheNextReservation(t *testing.T) {
	env := newReturnWindowEnv(t, clock.System{})
	kioskID, _ := fixtures.Kiosk(t, env.pool)
	deviceID := fixtures.AvailableDevice(t, env.pool)
	userID := fixtures.User(t, env.pool)
	_, deviceToken := fixtures.ActiveCredentialFor(t, env.pool, credentialsapi.SubjectDevice, deviceID)
	_, userToken := fixtures.ActiveCredentialFor(t, env.pool, credentialsapi.SubjectUser, userID)
	start := time.Now().UTC().Add(5 * time.Hour).Truncate(time.Second)
	env.reserve(t, deviceID, fixtures.User(t, env.pool), start, start.Add(time.Hour))

	_, r := env.borrowIn(t, kioskID, userToken, deviceToken, nil)
	if r.Outcome.Kind != checkoutapi.OutcomeBorrowed {
		t.Fatalf("outcome = %q, want borrowed", r.Outcome.Kind)
	}
	want := start.Add(-time.Hour)
	if r.Outcome.DueAt == nil || !r.Outcome.DueAt.Equal(want) {
		t.Fatalf("DueAt = %v, want %v (24h default clamped to next start minus gap)", r.Outcome.DueAt, want)
	}
	if r.Outcome.LatestReturnAt == nil || !r.Outcome.LatestReturnAt.Equal(want) {
		t.Fatalf("LatestReturnAt = %v, want %v", r.Outcome.LatestReturnAt, want)
	}
}

func TestBorrowUsesPreferredDueAtClampedToTheWindow(t *testing.T) {
	env := newReturnWindowEnv(t, clock.System{})
	kioskID, _ := fixtures.Kiosk(t, env.pool)
	userID := fixtures.User(t, env.pool)
	_, userToken := fixtures.ActiveCredentialFor(t, env.pool, credentialsapi.SubjectUser, userID)

	free := fixtures.AvailableDevice(t, env.pool)
	_, freeToken := fixtures.ActiveCredentialFor(t, env.pool, credentialsapi.SubjectDevice, free)
	preferred := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Minute)
	_, r := env.borrowIn(t, kioskID, userToken, freeToken, &preferred)
	if r.Outcome.DueAt == nil || !r.Outcome.DueAt.Equal(preferred) {
		t.Fatalf("free device DueAt = %v, want preferred %v", r.Outcome.DueAt, preferred)
	}

	busy := fixtures.AvailableDevice(t, env.pool)
	_, busyToken := fixtures.ActiveCredentialFor(t, env.pool, credentialsapi.SubjectDevice, busy)
	start := time.Now().UTC().Add(6 * time.Hour).Truncate(time.Second)
	env.reserve(t, busy, fixtures.User(t, env.pool), start, start.Add(time.Hour))
	_, r = env.borrowIn(t, kioskID, userToken, busyToken, &preferred)
	if r.Outcome.Kind != checkoutapi.OutcomeBorrowed {
		t.Fatalf("busy device outcome = %q, want borrowed (clamped, not refused)", r.Outcome.Kind)
	}
	if want := start.Add(-time.Hour); r.Outcome.DueAt == nil || !r.Outcome.DueAt.Equal(want) {
		t.Fatalf("busy device DueAt = %v, want %v", r.Outcome.DueAt, want)
	}
}

func TestWalkUpBorrowRefusedWhenTheNextReservationLeavesUnderThirtyMinutes(t *testing.T) {
	env := newReturnWindowEnv(t, clock.System{})
	kioskID, _ := fixtures.Kiosk(t, env.pool)
	deviceID := fixtures.AvailableDevice(t, env.pool)
	userID := fixtures.User(t, env.pool)
	_, deviceToken := fixtures.ActiveCredentialFor(t, env.pool, credentialsapi.SubjectDevice, deviceID)
	_, userToken := fixtures.ActiveCredentialFor(t, env.pool, credentialsapi.SubjectUser, userID)
	start := time.Now().UTC().Add(80 * time.Minute)
	env.reserve(t, deviceID, fixtures.User(t, env.pool), start, start.Add(time.Hour))

	_, r := env.borrowIn(t, kioskID, userToken, deviceToken, nil)
	if r.Outcome.Kind != checkoutapi.OutcomeRejected {
		t.Fatalf("outcome = %q, want rejected (80 min ahead < 60 min gap + 30 min)", r.Outcome.Kind)
	}
	if n, _ := env.lending.CountOpenByDevice(context.Background(), deviceID); n != 0 {
		t.Fatalf("open loans for device = %d, want 0", n)
	}
}

func TestCollectingAReservationDefaultsToItsEnd(t *testing.T) {
	env := newReturnWindowEnv(t, clock.System{})
	kioskID, _ := fixtures.Kiosk(t, env.pool)
	deviceID := fixtures.AvailableDevice(t, env.pool)
	userID := fixtures.User(t, env.pool)
	_, deviceToken := fixtures.ActiveCredentialFor(t, env.pool, credentialsapi.SubjectDevice, deviceID)
	_, userToken := fixtures.ActiveCredentialFor(t, env.pool, credentialsapi.SubjectUser, userID)
	now := time.Now().UTC().Truncate(time.Second)
	mine := env.reserve(t, deviceID, userID, now.Add(10*time.Minute), now.Add(5*time.Hour))

	_, r := env.borrowIn(t, kioskID, userToken, deviceToken, nil)
	if r.Outcome.Kind != checkoutapi.OutcomeReservationCollected {
		t.Fatalf("outcome = %q, want reservation_collected", r.Outcome.Kind)
	}
	if r.Outcome.DueAt == nil || !r.Outcome.DueAt.Equal(mine.EndAt) {
		t.Fatalf("DueAt = %v, want reservation end %v", r.Outcome.DueAt, mine.EndAt)
	}
}
```

- [ ] **Step 9: Run them**

Run: `cd hdms-backend && go test -race -tags=integration ./test/integration/ -run 'TestBorrow|TestWalkUp|TestCollecting'`
Expected: PASS (4 tests).

- [ ] **Step 10: Commit**

```bash
git add hdms-backend/internal/modules/checkout hdms-backend/test/integration/kiosk_return_window_test.go
git commit -m "feat(checkout): open kiosk loans with a reservation-aware due date

A borrow now defaults its expected return to the collected reservation's
end, the borrower's previous choice, the category period, or 24 hours,
and clamps it to the device's return window. The device lock is held
before the window is read so a concurrent staff booking cannot slip in."
```

---

### Task 3: Lending can change a loan's expected return

**Files:**
- Modify: `hdms-backend/queries/lending/lending.sql` (append)
- Modify: `hdms-backend/internal/modules/lending/module.go` (add method after `WriteOff`)
- Modify: `hdms-backend/internal/modules/lending/lendingapi/lending.go` (interface + meta type)
- Modify: `hdms-backend/internal/modules/checkout/deps.go` (`Loans` interface)
- Test: `hdms-backend/test/integration/lending_test.go` (append)

**Interfaces:**
- Produces: `lendingapi.DueChangeMeta{KioskID, Actor string}`
- Produces: `(*lending.Service).SetDueAt(ctx context.Context, loanID string, dueAt time.Time, meta lendingapi.DueChangeMeta) (lendingapi.Loan, error)` — `ErrLoanNotFound` if no loan, `ErrLoanNotOpen` if closed; records audit action `loan.due_changed` with payload `{"from": <old *time.Time>, "to": <new time.Time>, "kioskId": <string>}`.
- Produces: `checkout.Loans.SetDueAt` with the same signature.

- [ ] **Step 1: Write the failing integration test**

Append to `hdms-backend/test/integration/lending_test.go` (reuse the file's existing imports; add `lendingapi` and `audit` if missing):

```go
func TestSetDueAtChangesAnOpenLoanAndAuditsIt(t *testing.T) {
	pool := testdb.New(t)
	auditSvc := audit.New(pool)
	svc := lending.New(pool, auditSvc, clock.System{})
	ctx := context.Background()
	deviceID := fixtures.AvailableDevice(t, pool)
	userID := fixtures.User(t, pool)
	first := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	loan, err := svc.OpenLoan(ctx, deviceID, userID, &first, lendingapi.OpenMeta{Actor: "kiosk:k1", Source: "scanner"})
	if err != nil {
		t.Fatalf("OpenLoan: %v", err)
	}

	next := first.Add(48 * time.Hour)
	got, err := svc.SetDueAt(ctx, loan.ID, next, lendingapi.DueChangeMeta{KioskID: "", Actor: "kiosk:k1"})
	if err != nil {
		t.Fatalf("SetDueAt: %v", err)
	}
	if got.DueAt == nil || !got.DueAt.Equal(next) {
		t.Fatalf("DueAt = %v, want %v", got.DueAt, next)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE action = 'loan.due_changed' AND subject = $1`, "loan:"+loan.ID).Scan(&n); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if n != 1 {
		t.Fatalf("loan.due_changed events = %d, want 1", n)
	}

	if _, err := svc.CloseLoan(ctx, loan.ID, lendingapi.CloseMeta{Actor: "kiosk:k1", Source: "scanner"}); err != nil {
		t.Fatalf("CloseLoan: %v", err)
	}
	if _, err := svc.SetDueAt(ctx, loan.ID, next, lendingapi.DueChangeMeta{Actor: "kiosk:k1"}); !errors.Is(err, lendingapi.ErrLoanNotOpen) {
		t.Fatalf("SetDueAt on closed loan err = %v, want ErrLoanNotOpen", err)
	}
}
```

Check the audit table and column names with `grep -n "CREATE TABLE" hdms-backend/migrations/*audit*.sql` and adjust the query if they differ (e.g. `audit_log`).

- [ ] **Step 2: Run to verify it fails**

Run: `cd hdms-backend && go test -race -tags=integration ./test/integration/ -run TestSetDueAt`
Expected: FAIL to compile — `svc.SetDueAt undefined`, `lendingapi.DueChangeMeta undefined`.

- [ ] **Step 3: Add the query**

Append to `hdms-backend/queries/lending/lending.sql`:

```sql
-- name: SetLoanDueAt :one
UPDATE loans
SET due_at = $2
WHERE id = $1 AND status = 'open'
RETURNING id, device_id, user_id, status, origin, borrowed_at, due_at, returned_at,
    borrow_kiosk_id, return_kiosk_id, borrow_actor, return_actor, borrow_source, return_source,
    condition_out, condition_in, notes, session_id, paper_ref, recorded_at, recorded_by,
    backfill_note, disputed;
```

Run: `cd hdms-backend && sqlc generate`
Expected: `SetLoanDueAt(ctx, arg SetLoanDueAtParams) (Loan, error)` with fields `ID`, `DueAt`.

- [ ] **Step 4: Add the API type and service method**

In `hdms-backend/internal/modules/lending/lendingapi/lending.go`, next to `CloseMeta`:

```go
// DueChangeMeta attributes a change of a loan's expected return.
type DueChangeMeta struct {
	KioskID string // "" when not changed at a kiosk
	Actor   string // 'kiosk:<id>' | 'admin:<id>', required
}
```

and add to the `Service` interface, after `WriteOff`:

```go
	// SetDueAt changes an open loan's expected return. ErrLoanNotFound when
	// there is no such loan, ErrLoanNotOpen when it is already closed.
	// Callers that must respect reservations validate the new date and hold
	// db.LockDevice first; this method only records the change.
	SetDueAt(ctx context.Context, loanID string, dueAt time.Time, meta DueChangeMeta) (Loan, error)
```

In `hdms-backend/internal/modules/lending/module.go`, after `WriteOff`:

```go
func (s *Service) SetDueAt(ctx context.Context, loanID string, dueAt time.Time, meta lendingapi.DueChangeMeta) (lendingapi.Loan, error) {
	lid, err := pgtypeconv.UUID(loanID)
	if err != nil {
		return lendingapi.Loan{}, fmt.Errorf("lending: invalid loan id: %w", err)
	}
	var loan lendingapi.Loan
	err = db.NewTxManager(s.pool).Do(ctx, func(ctx context.Context) error {
		q := lendingstore.New(db.Conn(ctx, s.pool))
		prev, err := q.GetLoan(ctx, lid)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return lendingapi.ErrLoanNotFound
			}
			return err
		}
		row, err := q.SetLoanDueAt(ctx, lendingstore.SetLoanDueAtParams{ID: lid, DueAt: pgtypeconv.Timestamptz(dueAt)})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return lendingapi.ErrLoanNotOpen
			}
			return err
		}
		loan = toLoan(row)
		return s.audit.Record(ctx, auditapi.Event{
			Actor: meta.Actor, Action: "loan.due_changed", Subject: "loan:" + loan.ID,
			Payload: map[string]any{"from": pgtypeconv.TimePtr(prev.DueAt), "to": dueAt, "kioskId": meta.KioskID},
		})
	})
	if err != nil {
		return lendingapi.Loan{}, err
	}
	return loan, nil
}
```

If `q.GetLoan` returns a type other than `lendingstore.Loan` (check `internal/modules/lending/internal/store/lending.sql.go`), `prev.DueAt` is still a `pgtype.Timestamptz`; `pgtypeconv.TimePtr` is already used in this file (line ~698).

In `hdms-backend/internal/modules/checkout/deps.go`, add to `Loans` after `DueDateFor`:

```go
	SetDueAt(ctx context.Context, loanID string, dueAt time.Time, meta lendingapi.DueChangeMeta) (lendingapi.Loan, error)
```

- [ ] **Step 5: Run the test and build**

Run: `cd hdms-backend && go build ./... && go test -race -tags=integration ./test/integration/ -run TestSetDueAt`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add hdms-backend/queries/lending/lending.sql hdms-backend/internal/modules/lending hdms-backend/internal/modules/checkout/deps.go hdms-backend/test/integration/lending_test.go
git commit -m "feat(lending): change an open loan's expected return

SetDueAt updates due_at on an open loan and records loan.due_changed with
the previous and new values."
```

---

### Task 4: Checkout validates and applies a due-date change

**Files:**
- Modify: `hdms-backend/internal/modules/checkout/checkoutapi/checkout.go`
- Create: `hdms-backend/internal/modules/checkout/setduedate.go`
- Test: `hdms-backend/test/integration/kiosk_return_window_test.go` (append)

**Interfaces:**
- Consumes: `returnWindow` (Task 2), `Loans.SetDueAt` (Task 3), `db.LockDevice` (Task 1)
- Produces: `checkoutapi.LoanDueDate{LoanID string; DueAt time.Time; LatestReturnAt time.Time; SessionExpiresAt time.Time}` (zero `LatestReturnAt` = unbounded)
- Produces: `checkoutapi.ErrDueDateNotInFuture`; `*checkoutapi.DueDateConflictError{LatestReturnAt time.Time}`
- Produces: `checkoutapi.Service.SetLoanDueDate(ctx context.Context, sessionID, loanID string, dueAt time.Time, actor string) (LoanDueDate, error)`

- [ ] **Step 1: Write the failing integration tests**

Append to `hdms-backend/test/integration/kiosk_return_window_test.go` (add `errors` import):

```go
func TestSetLoanDueDateAcceptsUpToTheLatestAndRefusesBeyond(t *testing.T) {
	env := newReturnWindowEnv(t, clock.System{})
	ctx := context.Background()
	kioskID, _ := fixtures.Kiosk(t, env.pool)
	deviceID := fixtures.AvailableDevice(t, env.pool)
	userID := fixtures.User(t, env.pool)
	_, deviceToken := fixtures.ActiveCredentialFor(t, env.pool, credentialsapi.SubjectDevice, deviceID)
	_, userToken := fixtures.ActiveCredentialFor(t, env.pool, credentialsapi.SubjectUser, userID)
	start := time.Now().UTC().Add(30 * time.Hour).Truncate(time.Second)
	env.reserve(t, deviceID, fixtures.User(t, env.pool), start, start.Add(time.Hour))
	session, r := env.borrowIn(t, kioskID, userToken, deviceToken, nil)
	latest := start.Add(-time.Hour)

	got, err := env.checkout.SetLoanDueDate(ctx, session.ID, r.Outcome.LoanID, latest, "kiosk:"+kioskID)
	if err != nil {
		t.Fatalf("SetLoanDueDate at exactly latest: %v", err)
	}
	if !got.DueAt.Equal(latest) || !got.LatestReturnAt.Equal(latest) {
		t.Fatalf("result = %+v, want dueAt = latestReturnAt = %v", got, latest)
	}
	if !got.SessionExpiresAt.After(time.Now()) {
		t.Fatalf("SessionExpiresAt = %v, want refreshed into the future", got.SessionExpiresAt)
	}

	_, err = env.checkout.SetLoanDueDate(ctx, session.ID, r.Outcome.LoanID, latest.Add(time.Minute), "kiosk:"+kioskID)
	conflict, ok := errors.AsType[*checkoutapi.DueDateConflictError](err)
	if !ok || !conflict.LatestReturnAt.Equal(latest) {
		t.Fatalf("beyond latest err = %v, want DueDateConflictError{%v}", err, latest)
	}

	_, err = env.checkout.SetLoanDueDate(ctx, session.ID, r.Outcome.LoanID, time.Now().Add(-time.Minute), "kiosk:"+kioskID)
	if !errors.Is(err, checkoutapi.ErrDueDateNotInFuture) {
		t.Fatalf("past dueAt err = %v, want ErrDueDateNotInFuture", err)
	}
}

func TestSetLoanDueDateSeesAReservationBookedAfterTheBorrow(t *testing.T) {
	env := newReturnWindowEnv(t, clock.System{})
	ctx := context.Background()
	kioskID, _ := fixtures.Kiosk(t, env.pool)
	deviceID := fixtures.AvailableDevice(t, env.pool)
	userID := fixtures.User(t, env.pool)
	_, deviceToken := fixtures.ActiveCredentialFor(t, env.pool, credentialsapi.SubjectDevice, deviceID)
	_, userToken := fixtures.ActiveCredentialFor(t, env.pool, credentialsapi.SubjectUser, userID)
	session, r := env.borrowIn(t, kioskID, userToken, deviceToken, nil)

	start := time.Now().UTC().Add(40 * time.Hour).Truncate(time.Second)
	env.reserve(t, deviceID, fixtures.User(t, env.pool), start, start.Add(time.Hour))

	_, err := env.checkout.SetLoanDueDate(ctx, session.ID, r.Outcome.LoanID, start.Add(2*time.Hour), "kiosk:"+kioskID)
	conflict, ok := errors.AsType[*checkoutapi.DueDateConflictError](err)
	if !ok || !conflict.LatestReturnAt.Equal(start.Add(-time.Hour)) {
		t.Fatalf("err = %v, want conflict with latest %v", err, start.Add(-time.Hour))
	}
}

func TestSetLoanDueDateRefusesAnotherUsersLoan(t *testing.T) {
	env := newReturnWindowEnv(t, clock.System{})
	ctx := context.Background()
	kioskID, _ := fixtures.Kiosk(t, env.pool)
	deviceID := fixtures.AvailableDevice(t, env.pool)
	owner := fixtures.User(t, env.pool)
	_, deviceToken := fixtures.ActiveCredentialFor(t, env.pool, credentialsapi.SubjectDevice, deviceID)
	_, ownerToken := fixtures.ActiveCredentialFor(t, env.pool, credentialsapi.SubjectUser, owner)
	_, r := env.borrowIn(t, kioskID, ownerToken, deviceToken, nil)

	other := fixtures.User(t, env.pool)
	_, otherToken := fixtures.ActiveCredentialFor(t, env.pool, credentialsapi.SubjectUser, other)
	otherSession := createSession(t, ctx, env.checkout, kioskID)
	scan(t, ctx, env.checkout, otherSession.ID, otherToken)

	_, err := env.checkout.SetLoanDueDate(ctx, otherSession.ID, r.Outcome.LoanID, time.Now().Add(2*time.Hour), "kiosk:"+kioskID)
	if !errors.Is(err, checkoutapi.ErrSessionConflict) {
		t.Fatalf("err = %v, want ErrSessionConflict", err)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd hdms-backend && go test -race -tags=integration ./test/integration/ -run TestSetLoanDueDate`
Expected: FAIL to compile — `SetLoanDueDate undefined`.

- [ ] **Step 3: Add the API types**

In `hdms-backend/internal/modules/checkout/checkoutapi/checkout.go`, in the `var (...)` error block add:

```go
	// ErrDueDateNotInFuture: a loan's expected return must be after now.
	ErrDueDateNotInFuture = errors.New("checkout: expected return must be in the future")
```

After the error block add:

```go
// DueDateConflictError: the requested expected return is after the latest
// the device's return window allows (usually because a reservation was
// booked after the loan opened). LatestReturnAt is the current bound.
type DueDateConflictError struct {
	LatestReturnAt time.Time
}

func (e *DueDateConflictError) Error() string {
	return "checkout: expected return is after the latest allowed return"
}

// LoanDueDate is the result of changing a loan's expected return at the
// kiosk. LatestReturnAt is zero when no bound applies. SessionExpiresAt is
// the session's refreshed expiry, so the kiosk can keep its countdown in
// step with the server.
type LoanDueDate struct {
	LoanID           string
	DueAt            time.Time
	LatestReturnAt   time.Time
	SessionExpiresAt time.Time
}
```

Add to the `Service` interface after `ReturnLoan`:

```go
	// SetLoanDueDate changes the expected return of one of the session's
	// identified user's open loans. dueAt must be after now
	// (ErrDueDateNotInFuture) and no later than the device's return window
	// allows (*DueDateConflictError). Refreshes the session's expiry.
	SetLoanDueDate(ctx context.Context, sessionID, loanID string, dueAt time.Time, actor string) (LoanDueDate, error)
```

- [ ] **Step 4: Implement `setduedate.go`**

Create `hdms-backend/internal/modules/checkout/setduedate.go`:

```go
package checkout

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/checkout/checkoutapi"
	"github.com/hito-hospital/hdms/internal/modules/checkout/internal/machine"
	checkoutstore "github.com/hito-hospital/hdms/internal/modules/checkout/internal/store"
	"github.com/hito-hospital/hdms/internal/modules/lending/lendingapi"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/pgtypeconv"
	"github.com/jackc/pgx/v5"
)

// SetLoanDueDate lets the borrower change the expected return of a loan
// they just opened. Like ReturnLoan it acts only on the session's own
// user's open loans. The return window is recomputed under the device lock,
// so a staff booking made since the borrow is honoured. The session keeps
// its state; its expiry is refreshed because the borrower is still at the
// kiosk.
func (s *Service) SetLoanDueDate(ctx context.Context, sessionID, loanID string, dueAt time.Time, actor string) (checkoutapi.LoanDueDate, error) {
	sid, err := pgtypeconv.UUID(sessionID)
	if err != nil {
		return checkoutapi.LoanDueDate{}, fmt.Errorf("checkout: invalid session id: %w", err)
	}

	var result checkoutapi.LoanDueDate
	txErr := db.NewTxManager(s.pool).Do(ctx, func(ctx context.Context) error {
		q := checkoutstore.New(db.Conn(ctx, s.pool))
		session, err := q.GetSessionForUpdate(ctx, sid)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return checkoutapi.ErrSessionNotFound
			}
			return fmt.Errorf("checkout: get session: %w", err)
		}
		if session.ClosedAt.Valid {
			return checkoutapi.ErrSessionClosed
		}
		if !session.UserID.Valid {
			return checkoutapi.ErrSessionConflict
		}

		open, err := s.deps.Loans.OpenLoansFor(ctx, pgtypeconv.UUIDString(session.UserID))
		if err != nil {
			return fmt.Errorf("checkout: open loans for session user: %w", err)
		}
		var target *lendingapi.Loan
		for i := range open {
			if open[i].ID == loanID {
				target = &open[i]
				break
			}
		}
		if target == nil {
			return checkoutapi.ErrSessionConflict
		}

		now := s.clock.Now()
		if !dueAt.After(now) {
			return checkoutapi.ErrDueDateNotInFuture
		}
		if err := db.LockDevice(ctx, s.pool, target.DeviceID); err != nil {
			return fmt.Errorf("checkout: lock device to change due date: %w", err)
		}
		// The reservation this loan collected is already 'collected', so
		// it never counts as the next one.
		window, err := s.returnWindow(ctx, target.DeviceID, "", now)
		if err != nil {
			return err
		}
		if !window.Latest.IsZero() && dueAt.After(window.Latest) {
			return &checkoutapi.DueDateConflictError{LatestReturnAt: window.Latest}
		}

		loan, err := s.deps.Loans.SetDueAt(ctx, target.ID, dueAt, lendingapi.DueChangeMeta{
			KioskID: pgtypeconv.UUIDString(session.KioskID), Actor: actor,
		})
		if err != nil {
			return fmt.Errorf("checkout: set loan due date: %w", err)
		}

		newRow, err := s.persistSession(ctx, q, session,
			machine.Decision{NextState: machine.SessionState(session.State)}, machine.Input{},
			checkoutapi.ScanParams{Token: "due:" + loanID, Source: "manual"})
		if err != nil {
			return err
		}
		result = checkoutapi.LoanDueDate{
			LoanID: loan.ID, DueAt: *loan.DueAt, LatestReturnAt: window.Latest,
			SessionExpiresAt: pgtypeconv.Time(newRow.ExpiresAt),
		}
		return nil
	})
	if txErr != nil {
		return checkoutapi.LoanDueDate{}, txErr
	}
	return result, nil
}
```

`persistSession` with a zero `Action` falls to its `default` branch: the user and pending device stay as they are (`ClearPending` is false), and `expires_at` is recomputed for the unchanged state.

- [ ] **Step 5: Run the tests**

Run: `cd hdms-backend && go build ./... && go test -race -tags=integration ./test/integration/ -run 'TestSetLoanDueDate|TestBorrow|TestReturnWindow'`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add hdms-backend/internal/modules/checkout hdms-backend/test/integration/kiosk_return_window_test.go
git commit -m "feat(checkout): let the borrower change a kiosk loan's due date

SetLoanDueDate re-reads the return window under the device lock and
refuses a date in the past or past the latest allowed return, so a
reservation booked after the borrow still wins."
```

---

### Task 5: HTTP contract and route classification

**Files:**
- Modify: `hdms-backend/api/openapi.yaml` (paths after `/sessions/{id}/return-loan`; schemas `Outcome`, `ScanRequest`, new `SetSessionLoanDueDateRequest`, `SessionLoanDueDate`)
- Regenerate: `hdms-backend/internal/platform/httpx/gen/api.gen.go`, `hdms-frontend/packages/api-client/src/gen/*`
- Modify: `hdms-backend/internal/apiserver/sessions.go` (`SubmitScan`, `mapScanResult`, new handler)
- Modify: `hdms-backend/internal/apiserver/helpers.go` (`writeServiceError`)
- Modify: `hdms-backend/internal/platform/auth/kioskscope.go`, `hdms-backend/internal/platform/auth/roles.go`, `hdms-backend/internal/platform/httpx/metrics.go`
- Test: `hdms-backend/test/integration/sessions_http_test.go` (extend `TestHTTPSessionsWorkflow`)

**Interfaces:**
- Consumes: `checkoutapi.Service.SetLoanDueDate`, `DueDateConflictError`, `ErrDueDateNotInFuture` (Task 4); `ScanParams.PreferredDueAt`, `Outcome.LatestReturnAt` (Task 2)
- Produces (frontend, from codegen): `setSessionLoanDueDate({ path: { id }, body: { loanId, dueAt } })`, types `SessionLoanDueDate`, `SetSessionLoanDueDateRequest`; `Outcome.latestReturnAt?: string`; `ScanRequest.preferredDueAt?: string`

- [ ] **Step 1: Edit the OpenAPI spec**

In `hdms-backend/api/openapi.yaml`, after the `/sessions/{id}/return-loan` path block, add:

```yaml
  /sessions/{id}/loan-due-date:
    post:
      operationId: setSessionLoanDueDate
      summary: Change the expected return of one of the session user's open loans.
      description: >
        The new date must be in the future and no later than the device's
        return window allows (the booking gap before its next reservation,
        or the maximum loan length). Refreshes the session's expiry.
      tags: [sessions]
      security:
        - kioskToken: []
      parameters:
        - $ref: "#/components/parameters/IDParam"
        - $ref: "#/components/parameters/IdempotencyKey"
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: "#/components/schemas/SetSessionLoanDueDateRequest"
      responses:
        "200":
          description: The loan's new expected return and its current return window.
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/SessionLoanDueDate"
        default:
          $ref: "#/components/responses/ProblemResponse"
```

In `Outcome.properties`, after `dueAt`, add:

```yaml
        latestReturnAt:
          type: string
          format: date-time
          description: Latest expected return the borrower may choose (borrowed and reservation_collected only).
```

In `ScanRequest.properties`, after `scannedAt`, add:

```yaml
        preferredDueAt:
          type: string
          format: date-time
          description: Expected return the borrower chose for their previous device in this session; used as the default when a borrow follows.
```

After `ReturnSessionLoanRequest`, add:

```yaml
    SetSessionLoanDueDateRequest:
      type: object
      required: [loanId, dueAt]
      properties:
        loanId: { type: string }
        dueAt: { type: string, format: date-time }

    SessionLoanDueDate:
      type: object
      required: [loanId, dueAt, sessionExpiresAt]
      properties:
        loanId: { type: string }
        dueAt: { type: string, format: date-time }
        latestReturnAt: { type: string, format: date-time }
        sessionExpiresAt: { type: string, format: date-time }
```

- [ ] **Step 2: Regenerate**

Run (repo root): `task generate:backend && task generate:frontend`
Expected: `go build ./...` in `hdms-backend` now fails with `*Server does not implement gen.ServerInterface (missing method SetSessionLoanDueDate)`.

- [ ] **Step 3: Write the failing HTTP test**

The HTTP harness does not wire the reservations adapter, so this covers routing, kiosk scope, request/response shape and error mapping; the window logic is covered in Task 4. Extend `TestHTTPSessionsWorkflow` in `hdms-backend/test/integration/sessions_http_test.go`: insert this block between step "5. Scan Device (Borrow)" (after `loanID := scanRes.OpenLoans[0].Id`) and step "6. Return loan directly", and add `"time"` to the imports:

```go
	// 5b. Change the expected return, then try a date in the past.
	if scanRes.Outcome.DueAt == nil {
		t.Fatal("borrow outcome has no dueAt; every kiosk loan must carry one")
	}
	due := time.Now().UTC().Add(6 * time.Hour).Truncate(time.Minute)
	resp, data = postKiosk("/v1/sessions/"+sess.Id+"/loan-due-date", gen.SetSessionLoanDueDateRequest{
		LoanId: loanID, DueAt: due,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("SetSessionLoanDueDate status = %d: %s", resp.StatusCode, string(data))
	}
	var dueRes gen.SessionLoanDueDate
	if err := json.Unmarshal(data, &dueRes); err != nil {
		t.Fatalf("Unmarshal due-date result: %v", err)
	}
	if dueRes.LoanId != loanID || !dueRes.DueAt.Equal(due) || dueRes.SessionExpiresAt.IsZero() {
		t.Fatalf("due-date result = %+v, want loan %s due %v with a session expiry", dueRes, loanID, due)
	}

	resp, data = postKiosk("/v1/sessions/"+sess.Id+"/loan-due-date", gen.SetSessionLoanDueDateRequest{
		LoanId: loanID, DueAt: time.Now().UTC().Add(-time.Hour),
	})
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(string(data), "dueAt") {
		t.Fatalf("past dueAt: status = %d body = %s, want 422 naming dueAt", resp.StatusCode, string(data))
	}
```

Run: `cd hdms-backend && go test -race -tags=integration ./test/integration/ -run TestHTTPSessionsWorkflow`
Expected: FAIL to compile until the handler exists (Step 4), then FAIL with 404/405 on `/loan-due-date` until Step 5 classifies the route.

- [ ] **Step 4: Implement the handler, scan field and output field**

In `hdms-backend/internal/apiserver/sessions.go`:

`SubmitScan` — add `PreferredDueAt: body.PreferredDueAt,` to the `checkoutapi.ScanParams` literal.

`mapScanResult` — add `LatestReturnAt: res.Outcome.LatestReturnAt,` to the `gen.Outcome` literal.

After `ReturnSessionLoan` add:

```go
// SetSessionLoanDueDate changes the expected return of one of the session
// user's open loans.
func (s *Server) SetSessionLoanDueDate(w http.ResponseWriter, r *http.Request, id gen.IDParam, _ gen.SetSessionLoanDueDateParams) {
	body, ok := decodeJSON[gen.SetSessionLoanDueDateRequest](w, r)
	if !ok {
		return
	}

	if _, err := s.sessionForRequest(r, id); err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	res, err := s.checkout.SetLoanDueDate(r.Context(), id, body.LoanId, body.DueAt, actorFrom(r))
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	out := gen.SessionLoanDueDate{LoanId: res.LoanID, DueAt: res.DueAt, SessionExpiresAt: res.SessionExpiresAt}
	if !res.LatestReturnAt.IsZero() {
		out.LatestReturnAt = &res.LatestReturnAt
	}
	writeJSON(w, http.StatusOK, out)
}
```

Use the exact generated names from `api.gen.go` (`gen.SetSessionLoanDueDateParams`, field types for `DueAt`); if `DueAt` is generated as `time.Time` the snippet compiles as is.

In `hdms-backend/internal/apiserver/helpers.go`, `writeServiceError`, next to the existing `overlapErr` typed-error check before the `switch`, add:

```go
	if dueErr, ok := errors.AsType[*checkoutapi.DueDateConflictError](err); ok {
		p := httpx.NewProblem("due-date-conflict", "Expected return is too late", http.StatusConflict)
		p.Detail = "Another reservation needs this device; choose an earlier return."
		p.Extensions = map[string]any{"latestReturnAt": dueErr.LatestReturnAt}
		httpx.WriteProblem(w, r, p)
		return
	}
```

(match the style of the existing typed check — if it uses `errors.As` with a declared variable, do the same). In the switch's checkout section add:

```go
	case errors.Is(err, checkoutapi.ErrDueDateNotInFuture):
		writeValidationFailed(w, r, "dueAt must be in the future", []string{"dueAt"})
```

- [ ] **Step 5: Classify the route**

`hdms-backend/internal/platform/auth/kioskscope.go`, `KioskAllowedOperations`, after the return-loan entry:

```go
	"POST /v1/sessions/{id}/loan-due-date": {},
```

`hdms-backend/internal/platform/auth/roles.go`, after the return-loan entry:

```go
	"POST /v1/sessions/{id}/loan-due-date": "admin",
```

`hdms-backend/internal/platform/httpx/metrics.go`, after `{"POST", "/v1/sessions/{id}/return-loan"},`:

```go
	{"POST", "/v1/sessions/{id}/loan-due-date"},
```

Run `gofmt -w` on the three files so the map alignment is fixed.

- [ ] **Step 6: Run the HTTP, scope and role suites**

Run: `cd hdms-backend && go build ./... && go test -race -tags=integration ./test/integration/ -run 'TestHTTPSessions|Kiosk.*Scope|Scope|RoleMatrix|Idempotency|Metrics'`
Expected: PASS. If the problem JSON nests extensions (check the 409 body shape in `httpx/problem.go`), note the actual path to `latestReturnAt` for Task 7.

- [ ] **Step 7: Lint and commit**

Run: `task lint:backend`
Expected: no issues.

```bash
git add hdms-backend/api/openapi.yaml hdms-backend/internal hdms-backend/test/integration/sessions_http_test.go hdms-frontend/packages/api-client/src/gen
git commit -m "feat(api): kiosk endpoint to change a loan's expected return

POST /v1/sessions/{id}/loan-due-date returns the new due date, the
current latest allowed return and the refreshed session expiry. Scan
requests accept preferredDueAt and borrow outcomes carry latestReturnAt."
```

---

### Task 6: Kiosk return-date options

**Files:**
- Create: `hdms-frontend/apps/kiosk/src/lib/return-date-options.ts`
- Test: `hdms-frontend/apps/kiosk/src/lib/return-date-options.test.ts`

**Interfaces:**
- Produces:
  - `type ReturnChipId = "today" | "tomorrow" | "plus3" | "plus7" | "latest"`
  - `interface ReturnChip { id: ReturnChipId; at: Date; disabled: boolean }`
  - `returnChips(now: Date, latest: Date | null): ReturnChip[]`
  - `pickerDays(now: Date, latest: Date | null): Date[]` — local midnights from today to the latest day (31 days when `latest` is null)
  - `timeSlots(day: Date, now: Date, latest: Date | null): Date[]` — 15-minute steps on `day` strictly after `now` and `<= latest`
  - `END_OF_DAY_HOUR = 17`, `SLOT_MINUTES = 15`

- [ ] **Step 1: Write the failing tests**

Create `hdms-frontend/apps/kiosk/src/lib/return-date-options.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { pickerDays, returnChips, timeSlots } from "./return-date-options";

// All dates are local time: the kiosk shows the iPad's clock.
const local = (y: number, m: number, d: number, h = 0, min = 0) => new Date(y, m - 1, d, h, min);

describe("returnChips", () => {
  it("offers today, tomorrow, +3 and +1 week at 17:00 when unbounded", () => {
    const chips = returnChips(local(2026, 9, 29, 10), null);
    expect(chips.map((c) => c.id)).toEqual(["today", "tomorrow", "plus3", "plus7"]);
    expect(chips[0].at).toEqual(local(2026, 9, 29, 17));
    expect(chips[3].at).toEqual(local(2026, 10, 6, 17));
    expect(chips.every((c) => !c.disabled)).toBe(true);
  });

  it("hides today once 17:00 has passed", () => {
    const chips = returnChips(local(2026, 9, 29, 17, 30), null);
    expect(chips.map((c) => c.id)).toEqual(["tomorrow", "plus3", "plus7"]);
  });

  it("disables chips after the latest return", () => {
    const chips = returnChips(local(2026, 9, 29, 10), local(2026, 9, 30, 9));
    expect(chips.find((c) => c.id === "today")?.disabled).toBe(false);
    expect(chips.find((c) => c.id === "tomorrow")?.disabled).toBe(true);
    expect(chips.find((c) => c.id === "latest")).toBeUndefined();
  });

  it("adds a Latest chip first when every chip is disabled", () => {
    const latest = local(2026, 9, 29, 13);
    const chips = returnChips(local(2026, 9, 29, 12), latest);
    expect(chips[0]).toEqual({ id: "latest", at: latest, disabled: false });
    expect(chips.slice(1).every((c) => c.disabled)).toBe(true);
  });

  it("keeps a chip exactly at the latest return enabled", () => {
    const chips = returnChips(local(2026, 9, 29, 10), local(2026, 9, 29, 17));
    expect(chips.find((c) => c.id === "today")?.disabled).toBe(false);
  });
});

describe("pickerDays", () => {
  it("runs from today to the latest day", () => {
    const days = pickerDays(local(2026, 9, 29, 10), local(2026, 10, 2, 9));
    expect(days).toEqual([local(2026, 9, 29), local(2026, 9, 30), local(2026, 10, 1), local(2026, 10, 2)]);
  });

  it("offers 31 days when unbounded", () => {
    expect(pickerDays(local(2026, 9, 29, 10), null)).toHaveLength(31);
  });
});

describe("timeSlots", () => {
  it("starts at the next quarter hour after now and stops at the latest", () => {
    const slots = timeSlots(local(2026, 9, 29), local(2026, 9, 29, 12, 5), local(2026, 9, 29, 13));
    expect(slots).toEqual([
      local(2026, 9, 29, 12, 15),
      local(2026, 9, 29, 12, 30),
      local(2026, 9, 29, 12, 45),
      local(2026, 9, 29, 13, 0),
    ]);
  });

  it("covers the whole day for a future day", () => {
    const slots = timeSlots(local(2026, 9, 30), local(2026, 9, 29, 12), null);
    expect(slots[0]).toEqual(local(2026, 9, 30, 0, 0));
    expect(slots).toHaveLength(96);
  });
});
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd hdms-frontend && pnpm --filter kiosk test -- src/lib/return-date-options.test.ts`
Expected: FAIL — cannot resolve `./return-date-options`.

- [ ] **Step 3: Implement**

Create `hdms-frontend/apps/kiosk/src/lib/return-date-options.ts`:

```ts
// Return-date choices for the kiosk's "Return by" panel. Everything is in
// the kiosk's local time; the API carries ISO instants, so converting at
// the edges is enough.

export const END_OF_DAY_HOUR = 17;
export const SLOT_MINUTES = 15;
const UNBOUNDED_DAYS = 31;

export type ReturnChipId = "today" | "tomorrow" | "plus3" | "plus7" | "latest";

export interface ReturnChip {
  id: ReturnChipId;
  at: Date;
  disabled: boolean;
}

function atHour(base: Date, addDays: number, hour: number): Date {
  const d = new Date(base);
  d.setDate(d.getDate() + addDays);
  d.setHours(hour, 0, 0, 0);
  return d;
}

function startOfDay(d: Date): Date {
  return atHour(d, 0, 0);
}

export function returnChips(now: Date, latest: Date | null): ReturnChip[] {
  const candidates: { id: ReturnChipId; at: Date }[] = [
    { id: "today", at: atHour(now, 0, END_OF_DAY_HOUR) },
    { id: "tomorrow", at: atHour(now, 1, END_OF_DAY_HOUR) },
    { id: "plus3", at: atHour(now, 3, END_OF_DAY_HOUR) },
    { id: "plus7", at: atHour(now, 7, END_OF_DAY_HOUR) },
  ];
  const chips: ReturnChip[] = candidates
    .filter((c) => c.at.getTime() > now.getTime())
    .map((c) => ({ ...c, disabled: latest !== null && c.at.getTime() > latest.getTime() }));
  if (latest !== null && chips.every((c) => c.disabled)) {
    chips.unshift({ id: "latest", at: latest, disabled: false });
  }
  return chips;
}

export function pickerDays(now: Date, latest: Date | null): Date[] {
  const first = startOfDay(now);
  const last = latest ? startOfDay(latest) : atHour(now, UNBOUNDED_DAYS - 1, 0);
  const days: Date[] = [];
  for (let d = first; d.getTime() <= last.getTime(); d = atHour(d, 1, 0)) {
    days.push(d);
  }
  return days;
}

export function timeSlots(day: Date, now: Date, latest: Date | null): Date[] {
  const slots: Date[] = [];
  const start = startOfDay(day);
  for (let i = 0; i < (24 * 60) / SLOT_MINUTES; i++) {
    const slot = new Date(start);
    slot.setMinutes(i * SLOT_MINUTES);
    if (slot.getTime() <= now.getTime()) continue;
    if (latest !== null && slot.getTime() > latest.getTime()) break;
    slots.push(slot);
  }
  return slots;
}
```

- [ ] **Step 4: Run the tests**

Run: `cd hdms-frontend && pnpm --filter kiosk test -- src/lib/return-date-options.test.ts`
Expected: PASS (9 tests).

- [ ] **Step 5: Commit**

```bash
git add hdms-frontend/apps/kiosk/src/lib/return-date-options.ts hdms-frontend/apps/kiosk/src/lib/return-date-options.test.ts
git commit -m "feat(kiosk): return-date chip and time-slot options"
```

---

### Task 7: Kiosk session carries the chosen return date

**Files:**
- Modify: `hdms-frontend/apps/kiosk/src/machine/session-machine.ts`
- Modify: `hdms-frontend/apps/kiosk/src/machine/use-kiosk-session.ts:150-200,300-395`
- Create: `hdms-frontend/apps/kiosk/src/machine/return-date.test.ts`

**Interfaces:**
- Consumes: `setSessionLoanDueDate`, `SessionLoanDueDate` (Task 5 codegen)
- Produces:
  - `SessionContext.preferredDueAt: string | null`
  - `ScanActorInput.preferredDueAt?: string | null` (sent as `body.preferredDueAt`)
  - Event `{ type: "LOAN_DUE_UPDATED"; loanId: string; dueAt: string; latestReturnAt: string | null; sessionExpiresAt: string }` — handled in `ready` only
  - `type SetDueDateResult = { ok: true; loanId: string; dueAt: string; latestReturnAt: string | null; sessionExpiresAt: string } | { ok: false; conflict: true; latestReturnAt: string } | { ok: false; conflict: false }`
  - `executeSetDueDate(input: { sessionId: string; loanId: string; dueAt: string }): Promise<SetDueDateResult>`
  - `useKioskSession()` additionally returns `sessionId: string | null` and `applyDueUpdate(update: Extract<SetDueDateResult, { ok: true }>): void`

- [ ] **Step 1: Write the failing tests**

Create `hdms-frontend/apps/kiosk/src/machine/return-date.test.ts`:

```ts
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createActor } from "xstate";
import * as apiClient from "@hdms/api-client";
import type { ScanResult } from "@hdms/api-client";
import { buildKioskSessionMachine, executeScan, executeSetDueDate } from "./session-machine";

const borrowResult = (dueAt: string): ScanResult => ({
  session: {
    id: "sess-1",
    kioskId: "kiosk-1",
    state: "ready",
    user: { id: "u1", fullName: "Dr. Sharma", department: "Radiology", openLoanCount: 1 },
    startedAt: new Date().toISOString(),
    expiresAt: new Date(Date.now() + 25_000).toISOString(),
  },
  outcome: { kind: "borrowed", loanId: "loan-1", dueAt, latestReturnAt: "2026-10-01T08:00:00Z" },
  openLoans: [{ id: "loan-1", deviceId: "d1", assetTag: "A1", deviceName: "iPad", borrowedAt: new Date().toISOString(), dueAt }],
  message: { title: "", detail: "", tone: "success" },
});

describe("return date in the kiosk session", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    sessionStorage.clear();
    vi.clearAllMocks();
  });
  afterEach(() => vi.useRealTimers());

  it("LOAN_DUE_UPDATED updates the outcome, the open loan and the preferred date, and restarts ready's timer", () => {
    const actor = createActor(buildKioskSessionMachine()).start();
    actor.send({ type: "APPLY_SCAN_RESULT", result: borrowResult("2026-09-30T08:00:00Z") });
    expect(actor.getSnapshot().value).toBe("ready");

    vi.advanceTimersByTime(20_000);
    actor.send({
      type: "LOAN_DUE_UPDATED",
      loanId: "loan-1",
      dueAt: "2026-09-30T06:00:00Z",
      latestReturnAt: "2026-10-01T08:00:00Z",
      sessionExpiresAt: new Date(Date.now() + 25_000).toISOString(),
    });
    const ctx = actor.getSnapshot().context;
    expect(ctx.lastOutcome?.dueAt).toBe("2026-09-30T06:00:00Z");
    expect(ctx.openLoans[0].dueAt).toBe("2026-09-30T06:00:00Z");
    expect(ctx.preferredDueAt).toBe("2026-09-30T06:00:00Z");

    vi.advanceTimersByTime(20_000);
    expect(actor.getSnapshot().value).toBe("ready");
    vi.advanceTimersByTime(6_000);
    expect(actor.getSnapshot().value).toBe("idle");
    expect(actor.getSnapshot().context.preferredDueAt).toBeNull();
  });

  it("executeScan sends preferredDueAt", async () => {
    const spy = vi.spyOn(apiClient, "submitScan").mockResolvedValue({
      data: borrowResult("2026-09-30T08:00:00Z"),
      response: new Response(),
    } as any);
    await executeScan({ sessionId: "sess-1", kioskId: "kiosk-1", token: "t", source: "scanner", preferredDueAt: "2026-09-30T06:00:00Z" });
    expect(spy.mock.calls[0][0].body).toMatchObject({ preferredDueAt: "2026-09-30T06:00:00Z" });
  });

  it("executeSetDueDate maps a due-date-conflict problem", async () => {
    vi.spyOn(apiClient, "setSessionLoanDueDate").mockResolvedValue({
      data: undefined,
      error: { type: "https://hdms.example/problems/due-date-conflict", status: 409, latestReturnAt: "2026-09-30T04:00:00Z" },
      response: new Response(null, { status: 409 }),
    } as any);
    await expect(
      executeSetDueDate({ sessionId: "sess-1", loanId: "loan-1", dueAt: "2026-10-02T08:00:00Z" })
    ).resolves.toEqual({ ok: false, conflict: true, latestReturnAt: "2026-09-30T04:00:00Z" });
  });

  it("executeSetDueDate maps success", async () => {
    vi.spyOn(apiClient, "setSessionLoanDueDate").mockResolvedValue({
      data: { loanId: "loan-1", dueAt: "2026-09-30T06:00:00Z", sessionExpiresAt: "2026-09-29T10:00:25Z" },
      response: new Response(),
    } as any);
    await expect(
      executeSetDueDate({ sessionId: "sess-1", loanId: "loan-1", dueAt: "2026-09-30T06:00:00Z" })
    ).resolves.toEqual({ ok: true, loanId: "loan-1", dueAt: "2026-09-30T06:00:00Z", latestReturnAt: null, sessionExpiresAt: "2026-09-29T10:00:25Z" });
  });
});
```

If the machine's `APPLY_SCAN_RESULT` handling in `idle` does not move straight to `ready` for this result (check the existing tests in `lifecycle.test.ts` for how they reach `ready`), use the same approach those tests use.

- [ ] **Step 2: Run to verify it fails**

Run: `cd hdms-frontend && pnpm --filter kiosk test -- src/machine/return-date.test.ts`
Expected: FAIL — `executeSetDueDate` is not exported, `preferredDueAt` is undefined.

- [ ] **Step 3: Implement in `session-machine.ts`**

1. Import `setSessionLoanDueDate` from `@hdms/api-client`.
2. `SessionContext`: add `preferredDueAt: string | null;` with the comment `// The return date the borrower last chose this session; sent with the next device scan.` Add `preferredDueAt: null,` to `createInitialContext` and to `clearSessionContext`'s returned object.
3. `SessionMachineEvent`: add
   ```ts
   | { type: "LOAN_DUE_UPDATED"; loanId: string; dueAt: string; latestReturnAt: string | null; sessionExpiresAt: string }
   ```
4. `ScanActorInput`: add `preferredDueAt?: string | null;`. In `executeScan`'s `submitScan` body add `...(input.preferredDueAt ? { preferredDueAt: input.preferredDueAt } : {}),`.
5. After `executeReturnLoan` add:
   ```ts
   export type SetDueDateResult =
     | { ok: true; loanId: string; dueAt: string; latestReturnAt: string | null; sessionExpiresAt: string }
     | { ok: false; conflict: true; latestReturnAt: string }
     | { ok: false; conflict: false };

   export interface SetDueDateInput {
     sessionId: string;
     loanId: string;
     dueAt: string;
   }

   // Changing a return date is not a new transaction, so the result goes to
   // LOAN_DUE_UPDATED rather than APPLY_SCAN_RESULT.
   export async function executeSetDueDate(input: SetDueDateInput): Promise<SetDueDateResult> {
     try {
       const res = await setSessionLoanDueDate({
         path: { id: input.sessionId },
         body: { loanId: input.loanId, dueAt: input.dueAt },
         signal: getSessionAbortSignal(),
       });
       if (res.data) {
         return {
           ok: true,
           loanId: res.data.loanId,
           dueAt: res.data.dueAt,
           latestReturnAt: res.data.latestReturnAt ?? null,
           sessionExpiresAt: res.data.sessionExpiresAt,
         };
       }
       const problem = res.error as { type?: string; latestReturnAt?: string } | undefined;
       if (problem?.type?.endsWith("/due-date-conflict") && problem.latestReturnAt) {
         return { ok: false, conflict: true, latestReturnAt: problem.latestReturnAt };
       }
       return { ok: false, conflict: false };
     } catch {
       return { ok: false, conflict: false };
     }
   }
   ```
   If Task 5 Step 6 found `latestReturnAt` nested (e.g. under `extensions`), read it from there instead.
6. In the per-state loop, after `onTransitions["RESET"]` add:
   ```ts
   if (stateName === "ready") {
     // Re-entering ready restarts its timeout: the borrower is still here.
     onTransitions["LOAN_DUE_UPDATED"] = {
       target: "ready",
       reenter: true,
       actions: assign(({ context, event }: { context: SessionContext; event: any }) => ({
         ...context,
         preferredDueAt: event.dueAt,
         expiresAt: event.sessionExpiresAt,
         lastOutcome:
           context.lastOutcome?.loanId === event.loanId
             ? { ...context.lastOutcome, dueAt: event.dueAt, latestReturnAt: event.latestReturnAt ?? undefined }
             : context.lastOutcome,
         openLoans: context.openLoans.map((l) => (l.id === event.loanId ? { ...l, dueAt: event.dueAt } : l)),
       })),
     };
   }
   ```

- [ ] **Step 4: Implement in `use-kiosk-session.ts`**

1. In `handleScan`'s `executeScan({...})` call add `preferredDueAt: snapshot.context.preferredDueAt,`.
2. Replace the outcome sound key
   ```ts
   const outcomeKey = `outcome_${lastOutcome.kind}_${lastOutcome.device?.id ?? ""}_${lastOutcome.dueAt ?? ""}`;
   ```
   with
   ```ts
   // Keyed by loan so a changed return date never replays the borrow sound.
   const outcomeKey = `outcome_${lastOutcome.kind}_${lastOutcome.loanId ?? lastOutcome.device?.id ?? ""}`;
   ```
3. The effect that resets `isOutcomeDismissed` compares `lastOutcome` by reference; a `LOAN_DUE_UPDATED` creates a new object for the same loan. Change its condition so an outcome with the same `loanId` and `kind` as the previous one does not count as new:
   ```ts
   const isNewOutcome =
     lastOutcome !== lastOutcomeRef.current &&
     !(lastOutcome && lastOutcomeRef.current &&
       lastOutcome.loanId === lastOutcomeRef.current.loanId &&
       lastOutcome.kind === lastOutcomeRef.current.kind);
   if (isNewOutcome || lastProblem !== lastProblemRef.current) {
   ```
   keeping the body as is.
4. Add and return:
   ```ts
   const applyDueUpdate = React.useCallback(
     (update: Extract<SetDueDateResult, { ok: true }>) => {
       actor.send({ type: "LOAN_DUE_UPDATED", ...update });
     },
     [actor]
   );
   ```
   Return `applyDueUpdate` and `sessionId: snapshot.context.sessionId` from the hook alongside `dismissOutcome`. Import `type SetDueDateResult` from `./session-machine`.

- [ ] **Step 5: Run the kiosk machine tests**

Run: `cd hdms-frontend && pnpm --filter kiosk test -- src/machine`
Expected: PASS, including the existing `lifecycle`, `contract` and `resilience` tests.

- [ ] **Step 6: Commit**

```bash
git add hdms-frontend/apps/kiosk/src/machine
git commit -m "feat(kiosk): carry the borrower's chosen return date in the session

LOAN_DUE_UPDATED updates the outcome and open loan in place and restarts
the ready timeout; the next device scan sends the choice as
preferredDueAt. The borrow sound is keyed by loan so a date change never
replays it."
```

---

### Task 8: "Return by" panel on the success screen

**Files:**
- Create: `hdms-frontend/apps/kiosk/src/components/return-by-panel.tsx`
- Test: `hdms-frontend/apps/kiosk/src/components/return-by-panel.test.tsx`
- Modify: `hdms-frontend/apps/kiosk/src/screens/success-screen.tsx`
- Modify: `hdms-frontend/apps/kiosk/src/screens/success-screen.test.tsx`
- Modify: `hdms-frontend/apps/kiosk/src/routes/index.tsx:116-130`
- Modify: `hdms-frontend/apps/kiosk/src/i18n/en.ts`, `hdms-frontend/apps/kiosk/src/i18n/ja.ts`

**Interfaces:**
- Consumes: `returnChips`, `pickerDays`, `timeSlots` (Task 6); `executeSetDueDate`, `SetDueDateResult`, `useKioskSession().applyDueUpdate`, `.sessionId` (Task 7); `formatHumanDueDate` (`lib/date-format.ts`); `formatDate`, `formatTime` from `@hdms/i18n`
- Produces:
  ```ts
  interface ReturnByPanelProps {
    sessionId: string;
    loanId: string;
    dueAt: string;
    latestReturnAt: string | null;
    onUpdated: (update: Extract<SetDueDateResult, { ok: true }>) => void;
    onActivity: () => void;
    setDueDate?: (input: SetDueDateInput) => Promise<SetDueDateResult>; // defaults to executeSetDueDate
    now?: () => Date; // defaults to () => new Date()
  }
  ```
  `SuccessScreen` gains optional props `sessionId?: string | null`, `loanId?: string`, `latestReturnAt?: string | null`, `onDueUpdated?: ReturnByPanelProps["onUpdated"]`.

- [ ] **Step 1: Add the copy**

In `hdms-frontend/apps/kiosk/src/i18n/en.ts`, in `success`, add `borrowAutoDismissHint: "Scan another device, or tap Done (auto-closing in 12s)",` and add a new top-level section after `outcome`:

```ts
  returnBy: {
    title: "Return by",
    today: "Today 17:00",
    tomorrow: "Tomorrow 17:00",
    plus3: "In 3 days",
    plus7: "In 1 week",
    latest: "Latest: {time}",
    other: "Other…",
    pickDay: "Day",
    pickTime: "Time",
    saving: "Saving…",
    conflict: "A reservation needs this device soon. Latest return {date}, {time}.",
    failed: "Could not change the return date. Your current return date stays.",
  },
```

In `ja.ts`, same keys:

```ts
    borrowAutoDismissHint: "別の機器をスキャンするか、完了をタップしてください（12秒後に自動で閉じます）",
```

```ts
  returnBy: {
    title: "返却予定",
    today: "本日 17:00",
    tomorrow: "明日 17:00",
    plus3: "3日後",
    plus7: "1週間後",
    latest: "最終: {time}",
    other: "その他…",
    pickDay: "日付",
    pickTime: "時刻",
    saving: "保存中…",
    conflict: "この機器には近く予約があります。返却は {date} {time} までです。",
    failed: "返却予定を変更できませんでした。現在の返却予定のままです。",
  },
```

- [ ] **Step 2: Write the failing panel tests**

Create `hdms-frontend/apps/kiosk/src/components/return-by-panel.test.tsx`:

```tsx
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { axe } from "vitest-axe";
import { ReturnByPanel } from "./return-by-panel";

const now = () => new Date(2026, 8, 29, 10, 0); // 29 Sep 2026 10:00 local
const iso = (d: Date) => d.toISOString();

function renderPanel(overrides: Partial<React.ComponentProps<typeof ReturnByPanel>> = {}) {
  const props = {
    sessionId: "sess-1",
    loanId: "loan-1",
    dueAt: iso(new Date(2026, 8, 30, 10, 0)),
    latestReturnAt: null,
    onUpdated: vi.fn(),
    onActivity: vi.fn(),
    setDueDate: vi.fn(),
    now,
    ...overrides,
  };
  const utils = render(<ReturnByPanel {...props} />);
  return { ...utils, props };
}

describe("ReturnByPanel", () => {
  it("shows the loan's due date and chips, and passes axe", async () => {
    const { container } = renderPanel();
    expect(screen.getByTestId("return-by-current")).toHaveTextContent(/tomorrow/i);
    expect(screen.getByRole("button", { name: "Today 17:00" })).toBeEnabled();
    expect(await axe(container)).toHaveNoViolations();
  });

  it("disables chips after the latest return and offers a Latest chip when none fit", () => {
    renderPanel({ latestReturnAt: iso(new Date(2026, 8, 29, 13, 0)) });
    expect(screen.getByRole("button", { name: "Today 17:00" })).toBeDisabled();
    expect(screen.getByRole("button", { name: /Latest:/ })).toBeEnabled();
  });

  it("choosing a chip saves it and reports the update", async () => {
    const update = { ok: true as const, loanId: "loan-1", dueAt: iso(new Date(2026, 8, 29, 17)), latestReturnAt: null, sessionExpiresAt: iso(new Date()) };
    const setDueDate = vi.fn().mockResolvedValue(update);
    const { props } = renderPanel({ setDueDate });
    await userEvent.click(screen.getByRole("button", { name: "Today 17:00" }));
    expect(setDueDate).toHaveBeenCalledWith({ sessionId: "sess-1", loanId: "loan-1", dueAt: update.dueAt });
    await waitFor(() => expect(props.onUpdated).toHaveBeenCalledWith(update));
    expect(props.onActivity).toHaveBeenCalled();
  });

  it("a conflict moves the selection to the new latest and explains why", async () => {
    const latest = iso(new Date(2026, 8, 29, 15, 0));
    const setDueDate = vi
      .fn()
      .mockResolvedValueOnce({ ok: false, conflict: true, latestReturnAt: latest })
      .mockResolvedValueOnce({ ok: true, loanId: "loan-1", dueAt: latest, latestReturnAt: latest, sessionExpiresAt: iso(new Date()) });
    const { props } = renderPanel({ setDueDate });
    await userEvent.click(screen.getByRole("button", { name: "Tomorrow 17:00" }));
    await waitFor(() => expect(screen.getByTestId("return-by-message")).toHaveTextContent(/reservation needs this device/i));
    expect(setDueDate).toHaveBeenLastCalledWith({ sessionId: "sess-1", loanId: "loan-1", dueAt: latest });
    await waitFor(() => expect(props.onUpdated).toHaveBeenCalled());
  });

  it("a failure keeps the current date and says so", async () => {
    const setDueDate = vi.fn().mockResolvedValue({ ok: false, conflict: false });
    const { props } = renderPanel({ setDueDate });
    await userEvent.click(screen.getByRole("button", { name: "Today 17:00" }));
    await waitFor(() => expect(screen.getByTestId("return-by-message")).toHaveTextContent(/Could not change/));
    expect(props.onUpdated).not.toHaveBeenCalled();
  });

  it("Other… opens day and time choices bounded by the latest return", async () => {
    renderPanel({ latestReturnAt: iso(new Date(2026, 8, 29, 11, 0)) });
    await userEvent.click(screen.getByRole("button", { name: "Other…" }));
    const times = screen.getByRole("group", { name: "Time" });
    expect(times.querySelectorAll("button")).toHaveLength(4); // 10:15, 10:30, 10:45, 11:00
  });
});
```

- [ ] **Step 3: Run to verify it fails**

Run: `cd hdms-frontend && pnpm --filter kiosk test -- src/components/return-by-panel.test.tsx`
Expected: FAIL — cannot resolve `./return-by-panel`.

- [ ] **Step 4: Implement the panel**

Create `hdms-frontend/apps/kiosk/src/components/return-by-panel.tsx`:

```tsx
import * as React from "react";
import { formatDate, formatTime, useLocale } from "@hdms/i18n";
import { Button } from "@/components/ui/button";
import { useTranslator } from "@/i18n";
import { formatHumanDueDate } from "@/lib/date-format";
import { pickerDays, returnChips, timeSlots, type ReturnChip } from "@/lib/return-date-options";
import { executeSetDueDate, type SetDueDateInput, type SetDueDateResult } from "@/machine/session-machine";

export interface ReturnByPanelProps {
  sessionId: string;
  loanId: string;
  dueAt: string;
  latestReturnAt: string | null;
  onUpdated: (update: Extract<SetDueDateResult, { ok: true }>) => void;
  onActivity: () => void;
  setDueDate?: (input: SetDueDateInput) => Promise<SetDueDateResult>;
  now?: () => Date;
}

type Message = { kind: "conflict"; latest: Date } | { kind: "failed" } | null;

// The loan is already open with a default return date. This panel lets the
// borrower change it; walking away keeps the default.
export function ReturnByPanel({
  sessionId,
  loanId,
  dueAt,
  latestReturnAt,
  onUpdated,
  onActivity,
  setDueDate = executeSetDueDate,
  now = () => new Date(),
}: ReturnByPanelProps) {
  const t = useTranslator();
  const { locale } = useLocale();
  const [current, setCurrent] = React.useState(dueAt);
  const [latest, setLatest] = React.useState<Date | null>(latestReturnAt ? new Date(latestReturnAt) : null);
  const [saving, setSaving] = React.useState(false);
  const [message, setMessage] = React.useState<Message>(null);
  const [pickerOpen, setPickerOpen] = React.useState(false);
  const [day, setDay] = React.useState<Date | null>(null);

  React.useEffect(() => setCurrent(dueAt), [dueAt]);

  const choose = async (at: Date) => {
    onActivity();
    setSaving(true);
    setMessage(null);
    let result = await setDueDate({ sessionId, loanId, dueAt: at.toISOString() });
    if (!result.ok && result.conflict) {
      const bound = new Date(result.latestReturnAt);
      setLatest(bound);
      setMessage({ kind: "conflict", latest: bound });
      result = await setDueDate({ sessionId, loanId, dueAt: result.latestReturnAt });
    }
    setSaving(false);
    if (result.ok) {
      setCurrent(result.dueAt);
      setLatest(result.latestReturnAt ? new Date(result.latestReturnAt) : null);
      onUpdated(result);
      return;
    }
    if (!result.conflict) {
      setMessage({ kind: "failed" });
    }
  };

  const chipLabel = (chip: ReturnChip) =>
    chip.id === "latest" ? t("returnBy.latest", { time: formatTime(locale, chip.at) }) : t(`returnBy.${chip.id}`);

  const clock = now();
  const chips = returnChips(clock, latest);
  const days = pickerDays(clock, latest);
  const slots = day ? timeSlots(day, clock, latest) : [];

  return (
    <section
      data-testid="return-by-panel"
      aria-labelledby="return-by-title"
      className="w-full space-y-3 pt-3 border-t border-border/80"
      onPointerDown={onActivity}
    >
      <h2 id="return-by-title" className="text-lg font-bold text-foreground">
        {t("returnBy.title")}
      </h2>
      <p data-testid="return-by-current" className="text-sm font-semibold text-foreground/90" aria-live="polite">
        {saving ? t("returnBy.saving") : formatHumanDueDate(current, clock, locale)}
      </p>

      <div className="flex flex-wrap gap-2">
        {chips.map((chip) => (
          <Button
            key={chip.id}
            type="button"
            variant={new Date(current).getTime() === chip.at.getTime() ? "default" : "outline"}
            className="min-h-14 px-4 text-base"
            disabled={chip.disabled || saving}
            onClick={() => void choose(chip.at)}
          >
            {chipLabel(chip)}
          </Button>
        ))}
        <Button
          type="button"
          variant="outline"
          className="min-h-14 px-4 text-base"
          disabled={saving}
          aria-expanded={pickerOpen}
          onClick={() => {
            onActivity();
            setPickerOpen((open) => !open);
            setDay(days[0] ?? null);
          }}
        >
          {t("returnBy.other")}
        </Button>
      </div>

      {pickerOpen && (
        <div className="space-y-2">
          <div role="group" aria-label={t("returnBy.pickDay")} className="flex gap-2 overflow-x-auto pb-1">
            {days.map((d) => (
              <Button
                key={d.getTime()}
                type="button"
                variant={day?.getTime() === d.getTime() ? "default" : "outline"}
                className="min-h-14 shrink-0 px-3"
                onClick={() => {
                  onActivity();
                  setDay(d);
                }}
              >
                {formatDate(locale, d)}
              </Button>
            ))}
          </div>
          <div role="group" aria-label={t("returnBy.pickTime")} className="grid grid-cols-4 gap-2 max-h-56 overflow-y-auto">
            {slots.map((s) => (
              <Button
                key={s.getTime()}
                type="button"
                variant="outline"
                className="min-h-14"
                disabled={saving}
                onClick={() => void choose(s)}
              >
                {formatTime(locale, s)}
              </Button>
            ))}
          </div>
        </div>
      )}

      {message && (
        <p data-testid="return-by-message" role="status" className="text-sm font-semibold text-amber-700 dark:text-amber-400">
          {message.kind === "conflict"
            ? t("returnBy.conflict", { date: formatDate(locale, message.latest), time: formatTime(locale, message.latest) })
            : t("returnBy.failed")}
        </p>
      )}
    </section>
  );
}
```

Check before running: the `Button` import path matches other kiosk components (`grep -rn "components/ui/button" hdms-frontend/apps/kiosk/src | head -1`), `useLocale` comes from where `success-screen.tsx` imports it, and `t` accepts a params object the way `translate(catalogue, locale, key, params)` does. The `t(\`returnBy.${chip.id}\`)` template key may need a cast (`as "returnBy.today"`) if the translator's key type is a union; follow how other components build dynamic keys. The `no-literals` test must stay green: no user-visible string literals in this file.

- [ ] **Step 5: Run the panel tests**

Run: `cd hdms-frontend && pnpm --filter kiosk test -- src/components/return-by-panel.test.tsx`
Expected: PASS (6 tests). The Latest chip label in the tests is English because the test renders without a `LocaleProvider`; if the default locale is `ja`, wrap renders in `<LocaleProvider locale="en">` the way `success-screen.test.tsx` does (check its imports).

- [ ] **Step 6: Write the failing success-screen tests**

Append to `hdms-frontend/apps/kiosk/src/screens/success-screen.test.tsx` inside the `describe`:

```tsx
  it("shows the Return by panel for a borrow and waits 12s, reset by touches", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const onDone = vi.fn();
    render(
      <SuccessScreen
        kind="borrowed"
        device={{ id: "dev-1", name: "iPad", assetTag: "A1" }}
        dueAt={new Date(Date.now() + 86400000).toISOString()}
        sessionId="sess-1"
        loanId="loan-1"
        latestReturnAt={null}
        onDueUpdated={vi.fn()}
        onDone={onDone}
      />
    );
    expect(screen.getByTestId("return-by-panel")).toBeInTheDocument();
    act(() => vi.advanceTimersByTime(10_000));
    expect(onDone).not.toHaveBeenCalled();
    fireEvent.pointerDown(screen.getByTestId("return-by-panel"));
    act(() => vi.advanceTimersByTime(10_000));
    expect(onDone).not.toHaveBeenCalled();
    act(() => vi.advanceTimersByTime(2_500));
    expect(onDone).toHaveBeenCalledTimes(1);
    vi.useRealTimers();
  });

  it("keeps the 4s dismissal and no panel for a return", () => {
    vi.useFakeTimers();
    const onDone = vi.fn();
    render(<SuccessScreen kind="returned" device={{ id: "d", name: "iPad", assetTag: "A1" }} onDone={onDone} />);
    expect(screen.queryByTestId("return-by-panel")).not.toBeInTheDocument();
    act(() => vi.advanceTimersByTime(4_000));
    expect(onDone).toHaveBeenCalledTimes(1);
    vi.useRealTimers();
  });
```

Add `fireEvent` to the `@testing-library/react` import.

- [ ] **Step 7: Run to verify they fail**

Run: `cd hdms-frontend && pnpm --filter kiosk test -- src/screens/success-screen.test.tsx`
Expected: FAIL — no `return-by-panel`, and `onDone` fires at 4 s.

- [ ] **Step 8: Integrate the panel into `SuccessScreen`**

In `hdms-frontend/apps/kiosk/src/screens/success-screen.tsx`:

1. Add to `SuccessScreenProps`:
   ```ts
   sessionId?: string | null;
   loanId?: string;
   latestReturnAt?: string | null;
   onDueUpdated?: ReturnByPanelProps["onUpdated"];
   ```
   and destructure them. Import `ReturnByPanel, type ReturnByPanelProps` from `@/components/return-by-panel`.
2. Replace the auto-dismiss effect with:
   ```tsx
   const canChangeReturn = !isReturn && !!sessionId && !!loanId && !!dueAt && !!onDueUpdated;
   const dismissAfterMs = canChangeReturn ? 12_000 : 4_000;
   // Each touch in the Return by panel restarts the countdown.
   const [activity, setActivity] = React.useState(0);
   const noteActivity = React.useCallback(() => setActivity((n) => n + 1), []);

   React.useEffect(() => {
     const timer = setTimeout(() => {
       onDone();
     }, dismissAfterMs);
     return () => clearTimeout(timer);
   }, [onDone, dismissAfterMs, activity]);
   ```
3. Replace the `{dueLine && (...)}` block with:
   ```tsx
   {canChangeReturn ? (
     <ReturnByPanel
       sessionId={sessionId!}
       loanId={loanId!}
       dueAt={dueAt!}
       latestReturnAt={latestReturnAt ?? null}
       onUpdated={onDueUpdated!}
       onActivity={noteActivity}
     />
   ) : (
     dueLine && (
       <div data-testid="success-due-line" className="w-full pt-3 border-t border-border/80 text-center">
         <p className="text-sm font-semibold text-foreground/90 bg-muted/60 py-1.5 px-3 rounded-lg">{dueLine}</p>
       </div>
     )
   )}
   ```
4. Change the hint to `{t(canChangeReturn ? "success.borrowAutoDismissHint" : "success.autoDismissHint")}`.

- [ ] **Step 9: Wire it in the route**

In `hdms-frontend/apps/kiosk/src/routes/index.tsx`, destructure `sessionId` and `applyDueUpdate` from the `useKioskSession()` result (next to `dismissOutcome`), and add to the `<SuccessScreen ...>` props:

```tsx
          sessionId={sessionId}
          loanId={outcomeView.outcome.loanId}
          latestReturnAt={outcomeView.outcome.latestReturnAt ?? null}
          onDueUpdated={applyDueUpdate}
```

- [ ] **Step 10: Run the kiosk suite and build**

Run: `cd hdms-frontend && pnpm --filter kiosk test && pnpm -w build && task lint:frontend`
Expected: all kiosk tests PASS (including `no-literals`, `visual-design`, `touch-targets`, `hostile-props`), build and lint clean. If `touch-targets.test.tsx` enumerates screens, add the Return by panel's buttons to it only if it fails for them; they already use `min-h-14`.

- [ ] **Step 11: Commit**

```bash
git add hdms-frontend/apps/kiosk/src
git commit -m "feat(kiosk): let borrowers choose the return date after a borrow

The success screen for borrows and reservation collections shows a
Return by panel with quick choices and a day/time picker bounded by the
latest allowed return. Each change is saved immediately; a reservation
booked meanwhile moves the choice to the new latest and says why."
```

---

### Task 9: End-to-end journey and documentation

**Files:**
- Modify: `hdms-frontend/e2e/helpers/test-api.ts` (add `seedReservation`)
- Modify: `hdms-frontend/e2e/kiosk.spec.ts` (add E21)
- Modify: `hdms-frontend/e2e/README.md`
- Modify: `docs/04-scanning-and-checkout-flows.md`, `docs/07-kiosk-app.md`, `docs/phases/phase-6/6.4-reservations.md`

**Interfaces:**
- Consumes: everything above; admin `POST /v1/reservations` (`CreateReservationRequest{deviceId, userId, startAt, endAt}`).
- Produces: `TestApiClient.seedReservation(params: { deviceId: string; userId: string; startAt: Date; endAt: Date }): Promise<{ id: string }>`

- [ ] **Step 1: Add the reservation seed helper**

In `hdms-frontend/e2e/helpers/test-api.ts`, inside `TestApiClient` after `seedDevice`:

```ts
  async seedReservation(params: { deviceId: string; userId: string; startAt: Date; endAt: Date }): Promise<{ id: string }> {
    const res = await this.request("/v1/reservations", {
      method: "POST",
      body: JSON.stringify({
        deviceId: params.deviceId,
        userId: params.userId,
        startAt: params.startAt.toISOString(),
        endAt: params.endAt.toISOString(),
      }),
    });
    if (!res.ok) {
      throw new Error(`Create reservation failed: ${await res.text()}`);
    }
    return (await res.json()) as { id: string };
  }
```

- [ ] **Step 2: Write the e2e scenario**

Append to `hdms-frontend/e2e/kiosk.spec.ts` inside the describe block that holds E1–E20 (it provides `api`, `page` and the pre-paired kiosk):

```ts
  test("E21_ReturnDateStopsBeforeTheNextReservation", async ({ page }) => {
    const borrower: TestUser = await api.seedUser();
    const reserver: TestUser = await api.seedUser();
    const device: TestDevice = await api.seedDevice();
    const start = new Date();
    start.setDate(start.getDate() + 1);
    start.setHours(10, 0, 0, 0);
    await api.seedReservation({ deviceId: device.id, userId: reserver.id, startAt: start, endAt: new Date(start.getTime() + 60 * 60 * 1000) });

    await page.goto("/");
    await expect(page.getByTestId("idle-prompt")).toBeVisible({ timeout: 10_000 });
    await simulateScan(page, borrower.token);
    await simulateScan(page, device.token);
    await expect(page.getByTestId("return-by-panel")).toBeVisible({ timeout: 5_000 });

    // Tomorrow 17:00 is after 09:00 (10:00 minus the 60-minute gap).
    await expect(page.getByRole("button", { name: ja.returnBy.tomorrow })).toBeDisabled();

    await page.getByRole("button", { name: ja.returnBy.other }).click();
    const lastSlot = page.getByRole("group", { name: ja.returnBy.pickTime }).getByRole("button").last();
    // Pick tomorrow in the day row, then the last time offered must be 09:00.
    await page.getByRole("group", { name: ja.returnBy.pickDay }).getByRole("button").last().click();
    await expect(lastSlot).toHaveText(/9:00|09:00/);
    await lastSlot.click();

    await expect
      .poll(async () => {
        const loans = await api.getDeviceLoans(device.id);
        const open = loans.find((l: any) => l.status === "open");
        return open ? new Date(open.dueAt).getTime() : null;
      })
      .toBe(new Date(start.getTime() - 60 * 60 * 1000).getTime());

    const a11y = await new AxeBuilder({ page }).analyze();
    expect(a11y.violations).toEqual([]);
  });
```

Match the scan order to an existing user-then-device test (E2) if the kiosk needs the user first; the success screen appears after the second scan either way. If the reservation's day row button is not last because `pickerDays` ends on the latest day, it is last by construction — keep it.

- [ ] **Step 3: Run the e2e scenario**

Run: `cd hdms-frontend && pnpm exec playwright test e2e/kiosk.spec.ts -g E21` (follow `e2e/README.md` for starting the stack first).
Expected: PASS. Then run the full kiosk e2e file once: `pnpm exec playwright test e2e/kiosk.spec.ts` — Expected: PASS (E1–E21).

- [ ] **Step 4: Update the docs**

- `hdms-frontend/e2e/README.md`: add `- \`E21_ReturnDateStopsBeforeTheNextReservation\`` under Kiosk Journeys.
- `docs/04-scanning-and-checkout-flows.md`: in the borrow flow, add a short subsection "Expected return date": the loan opens with a default due date (collected reservation end, previous choice this session, category period, or 24 hours), clamped to the return window (`next active reservation start − booking gap`, `borrow + maximum booking length`); a reservation starting within `max(pre-window, gap + 30 minutes)` is treated as in force (walk-ups refused, reserver collects early); the borrower changes the date on the success screen via `POST /v1/sessions/{id}/loan-due-date`, which re-checks the window and answers `409 due-date-conflict` with `latestReturnAt`.
- `docs/07-kiosk-app.md`: under the success screen, describe the Return by panel (chips, Other… day/time picker in 15-minute steps, disabled chips past the latest return, Latest chip, immediate save, conflict message, 12-second dismissal reset by touch, returns unchanged at 4 seconds).
- `docs/phases/phase-6/6.4-reservations.md`: append to the implementation note: "Kiosk loans respect the same gap: a walk-up loan's expected return is capped at the next active reservation's start minus the gap, and the borrower can only move it within that bound. Every kiosk loan now has an expected return, so devices borrowed at the kiosk remain bookable from the staff app."

Grep every endpoint, setting and file name you wrote into the docs to confirm it exists: `grep -rn "loan-due-date" hdms-backend/api/openapi.yaml`, `grep -n "ReturnBufferMinutes\|MaxDurationDays" hdms-backend/internal/platform/settings/service.go`.

- [ ] **Step 5: Full gate**

Run:
```bash
cd hdms-backend && go test ./... && go test -race -tags=integration ./test/... && cd ..
task lint
cd hdms-frontend && pnpm -r test && pnpm -w build
```
Expected: all green. Report any failure with its output; do not mark the task done on a red gate.

- [ ] **Step 6: Commit**

```bash
git add hdms-frontend/e2e docs
git commit -m "test(e2e): kiosk return date stops before the next reservation

Also documents the return window, the loan-due-date endpoint and the
kiosk Return by panel."
```
