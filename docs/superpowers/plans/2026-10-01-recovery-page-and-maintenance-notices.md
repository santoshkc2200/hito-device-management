# Recovery Page and Maintenance Notices Implementation Plan (plan 3b)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the hospital admin a browser page at `/recovery` that restores HDMS through the worker's recovery API using only the recovery key, and make the kiosk, staff app and admin console show a plain "under maintenance" notice while a restore runs.

**Architecture:** A new Vite app `hdms-frontend/apps/recovery` talks only to `/recovery/api/*` (plan 3a's worker routes); Caddy serves it at `/recovery/` and forwards `/recovery/api/*` to `worker:8090`. While a restore holds maintenance mode the API answers `503` with problem type `maintenance`; one shared store in `@hdms/ui` records that, every app swaps its screen for a notice, and the store asks `GET /v1/readyz` every 15 seconds — `readyz` now answers `503 maintenance` too, and a `200` there ends the notice. The kiosk detects maintenance inside `resilientFetch`, before its retry logic, so a restore never flips it to the offline screen.

**Tech Stack:** Go 1.26 (one handler change), React 19, Vite 8, TypeScript 6, vitest 4 + Testing Library + vitest-axe, Tailwind 4, `@hdms/i18n`, Caddy 2.

**Spec:** `docs/superpowers/specs/2026-09-30-guided-backup-and-disaster-recovery-design.md` — sections "Recovery page" (App, Flow), "Architecture" (caddy routes), "Security and trust boundaries", "Testing → Frontend" (plan 3 of 4, second half). The maintenance notices come from `docs/superpowers/specs/2026-09-30-backup-console-and-job-worker-design.md`, section "Maintenance on the other clients". Plan 3a (`docs/superpowers/plans/2026-10-01-recovery-engine-and-api.md`, merged as `2a7719e`) built the worker routes and the API maintenance gate this plan consumes.

**Base:** branch from `main` at `2a7719e` or later.

**Not in this plan:** `install.sh`, `docs/runbooks/disaster-recovery.md` and the `production-deployment.md` changes (plan 4); the console restore and **Discard** of the kept database (the 09-30 spec's restore plan); closing SSE streams already open when maintenance starts.

## Decisions made while planning (not in the spec)

1. **Clients re-check maintenance through `GET /v1/readyz`, not a new endpoint.** `readyz` is already public, kiosk-allowed, exempt from the maintenance gate and the rate limiter, and listed in the metrics routes. It now answers `503` with problem type `maintenance` while `system_state.maintenance` is on; any `200` ends a client's notice, and anything else (including `not-ready` while the swap terminates connections, or a network error) keeps it. No compose or Caddy healthcheck uses `readyz`, so nothing restarts during a restore.
2. **One maintenance store in `@hdms/ui`** (`reportMaintenance`, `clearMaintenance`, `useMaintenance`, `isMaintenanceResponse`, `installMaintenanceInterceptor`). Staff and admin install a response interceptor on the generated client. The kiosk checks inside `resilientFetch` instead: its retry loop would otherwise retry a 503 three times (about 7 s) and then count a connectivity failure, and two of those flip it to the offline screen.
3. **The kiosk drops its open session when maintenance starts** (abort in-flight requests, clear the stored session id, `RESET` the machine) and ignores scans until it ends. Nothing is queued: the offline queue has no production enqueue path today, and the replay engine already holds a queued item on any 5xx — a test pins that for the maintenance response.
4. **Admin shows the notice on every route except `/backups` and `/login`** (sign-in is exempt from the gate, so an admin can reach Backups). **Staff's auth guard does not redirect to `/login` while maintenance is on**; it rethrows, the root shows the notice, and when maintenance ends the app invalidates its queries and its router so the guard runs again.
5. **The recovery app has no router, no service worker and no generated client.** The recovery API is not in the OpenAPI contract (the API process is not involved); `src/lib/api.ts` mirrors `hdms-backend/internal/platform/recovery/handler.go`. Default locale is Japanese with a language toggle.
6. **Recovery app details:** the key stays in the field after a typo or a wrong key (retyping 28 characters is how mistakes multiply) and is cleared once unlocked or on a keys mismatch; the typed confirmation is compared case-insensitively and always sent as `RESTORE`; the "Open HDMS admin" link is `/admin/` (the production path).
7. **The kiosk service worker must not answer navigations to `/admin`, `/staff` or `/recovery`.** Its scope is `/` and its `navigateFallback` is `index.html`, so a desktop browser that once opened the kiosk would be shown the kiosk instead of the recovery page. `navigateFallbackDenylist` fixes that for all three.
8. **Dev routing:** the recovery Vite server runs on `:5176` and proxies `/recovery/api` to the dev Caddy on `:8443`, which forwards to `worker:8090` (the worker port is not published). The session cookie is `Secure`, so the dev server needs the mkcert certificates like the other apps.

## Global Constraints

- Maintenance answer (gate and `readyz` alike): `503`, problem type suffix `/maintenance`, `Retry-After: 15`, written by one helper, `writeMaintenance`.
- Clients re-check `GET /v1/readyz` every 15 seconds (`MAINTENANCE_RECHECK_MS = 15_000`); only a `200` ends the notice.
- Kiosk: a maintenance 503 is never retried, never recorded as a connectivity failure, never enqueued; maintenance outranks the offline screen.
- Admin: notice on every route except `/backups` and `/login`. Staff: full-page notice on every route.
- New kiosk Japanese strings use only glyphs already in the bundled Noto Sans JP subset (`hdms-frontend/packages/ui/src/fonts/`); Task 3 checks this before the catalogue changes.
- Every user-visible string lives in the app's `i18n/en.ts` and `i18n/ja.ts`; the kiosk, admin and recovery apps each run a `no-literals` test.
- The recovery app calls only `/recovery/api/*` with `credentials: "same-origin"`, never `/v1`. Progress polls every 2 seconds (`POLL_MS = 2000`). Built with `VITE_BASE_PATH=/recovery/`.
- Caddy forwards `/recovery/api/*` to `worker:8090` with `header_up X-Forwarded-For {http.request.remote.host}` (the worker's rate limit keys on `httpx.ClientIP`, which trusts the first `X-Forwarded-For` hop); it never routes `/internal/*`, and it never injects security headers.
- Frontend gate before every frontend commit, from `hdms-frontend`: `pnpm -r lint`, `pnpm -r test`, `pnpm -r build` — all three, `build` included (it runs `tsc -b`, which `test` does not).
- Backend gate before a backend commit (from `hdms-backend`): `gofmt -l .` prints nothing and `golangci-lint run ./...` reports 0 issues.
- Run integration and frontend suites one after another, never in parallel (parallel runs cause false timeouts on this machine).

## Review Focus

1. **A spoofed `X-Forwarded-For` on `/recovery/api/unlock`.** If Caddy appends instead of replacing, an attacker rotates the header and gets 5 fresh attempts per minute per made-up IP (only the 20/hour global cap remains). Expected: the worker logs Caddy's peer address whatever the client sends. Pinned in Task 8 (adapted-config check and the live log check).
2. **An admin laptop that once opened the kiosk at `/`.** The kiosk service worker would serve the kiosk app for `/recovery/`, so the recovery page is unreachable exactly when it is needed. Expected: the page loads. Pinned in Task 8 (`navigateFallbackDenylist` test in `pwa-kiosk.test.tsx`).
3. **A kiosk in the middle of a borrow when the restore switches maintenance on.** Today its retries would turn into "Reconnecting to hospital network" after a few seconds. Expected: the maintenance screen with the paper-register instruction, the session dropped, and back to idle by itself afterwards. Pinned in Task 3 (`aMaintenance503IsNeitherRetriedNorCountedAsOffline`, `routes/maintenance.test.tsx`).
4. **A signed-in staff member who opens the staff app during a restore.** `/v1/staff/me` is gated, so the auth guard would send them to the login page and leave them there. Expected: the maintenance notice, the same URL, and the app reloading itself when maintenance ends. Pinned in Task 4 (`staff app under maintenance`).
5. **The worker restarts, or the 30-minute session lapses, while the admin watches the progress screen.** Expected: back to key entry with "Your recovery session ended", and after unlocking again the page picks up the running restore instead of offering a second one. Pinned in Task 7 (`returns to key entry when the session is lost mid-restore and picks the restore up again`).

## File Structure

| File | Responsibility |
|---|---|
| `hdms-backend/internal/apiserver/maintenance.go` (modify) | `writeMaintenance`, the one maintenance answer |
| `hdms-backend/internal/apiserver/server.go` (modify) | `GetReadyz` answers `maintenance` |
| `hdms-backend/api/openapi.yaml` (modify) | `readyz` 503 description; regenerated Go and TS clients |
| `hdms-frontend/packages/ui/src/maintenance.ts` (create) | Shared maintenance store, re-check loop, hook, interceptor |
| `hdms-frontend/apps/kiosk/src/lib/api.ts` (modify) | `resilientFetch` short-circuits a maintenance 503 |
| `hdms-frontend/apps/kiosk/src/screens/maintenance-screen.tsx` (create) | Kiosk maintenance screen |
| `hdms-frontend/apps/kiosk/src/machine/use-kiosk-session.ts` (modify) | Drop the session on maintenance, ignore scans |
| `hdms-frontend/apps/kiosk/src/routes/index.tsx` (modify) | Maintenance screen outranks every other screen |
| `hdms-frontend/apps/kiosk/vite.config.ts` (modify) | `navigateFallbackDenylist` |
| `hdms-frontend/apps/staff/src/components/maintenance-notice.tsx` (create) | Staff notice |
| `hdms-frontend/apps/staff/src/routes/root.tsx`, `routes/authenticated.tsx`, `App.tsx` (modify) | Notice, no login redirect during maintenance, interceptor |
| `hdms-frontend/apps/admin/src/components/maintenance-notice.tsx` (create) | Admin notice with a link to Backups |
| `hdms-frontend/apps/admin/src/routes/root.tsx`, `App.tsx` (modify) | Notice except on `/backups` and `/login`, interceptor |
| `hdms-frontend/apps/recovery/**` (create) | The recovery app |
| `deploy/production/Caddyfile`, `deploy/production/Dockerfile.caddy`, `deploy/Caddyfile`, `docker-compose.yml`, `deploy/production/smoke.sh`, `Taskfile.yml` (modify) | Routing, build, dev wiring, smoke check |

---

### Task 1: `/v1/readyz` reports maintenance

**Files:**
- Modify: `hdms-backend/internal/apiserver/maintenance.go`, `hdms-backend/internal/apiserver/server.go:111-124`, `hdms-backend/api/openapi.yaml` (the `/readyz` 503 response)
- Regenerate: `hdms-backend/internal/platform/httpx/gen/api.gen.go`, `hdms-frontend/packages/api-client/src/gen/**`
- Test: `hdms-backend/test/integration/readiness_test.go`

**Interfaces:**
- Consumes: `backup.GetMaintenance(ctx, q) (backup.Maintenance, error)`, `backup.SetMaintenance(ctx, q, on bool, reason string) error` (plan 3a).
- Produces: `GET /v1/readyz` → `200 {"status":"ok"}` when ready; `503` problem `not-ready` when the database is down or not current; `503` problem `maintenance` with `Retry-After: 15` while maintenance is on. Tasks 2–4 rely on exactly this.

- [ ] **Step 1: Write the failing test**

In `hdms-backend/test/integration/readiness_test.go`, replace the import block and `readyzStatus` with:

```go
import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hito-hospital/hdms/internal/apiserver"
	"github.com/hito-hospital/hdms/internal/platform/backup"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/test/testdb"
)

func readyz(t *testing.T, pool *db.Pool) *httptest.ResponseRecorder {
	t.Helper()
	srv := apiserver.New(pool, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, "test", apiserver.BackupConsoleConfig{})
	rec := httptest.NewRecorder()
	srv.GetReadyz(rec, httptest.NewRequest(http.MethodGet, "/v1/readyz", nil))
	return rec
}

func readyzStatus(t *testing.T, pool *db.Pool) int {
	t.Helper()
	return readyz(t, pool).Code
}
```

Append:

```go
// Every client showing the maintenance notice asks /v1/readyz every 15
// seconds; it must say "maintenance" for as long as a restore holds the
// switch, and 200 the moment it lets go.
func TestReadyzReportsMaintenance(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	if got := readyzStatus(t, pool); got != http.StatusOK {
		t.Fatalf("readyz before maintenance = %d, want 200", got)
	}

	if err := backup.SetMaintenance(ctx, pool.Pool, true, "restore"); err != nil {
		t.Fatal(err)
	}
	rec := readyz(t, pool)
	var problem struct{ Type string }
	_ = json.Unmarshal(rec.Body.Bytes(), &problem)
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") != "15" || !strings.HasSuffix(problem.Type, "/maintenance") {
		t.Fatalf("readyz during maintenance = %d, type %q, Retry-After %q; want 503, …/maintenance, 15",
			rec.Code, problem.Type, rec.Header().Get("Retry-After"))
	}

	if err := backup.SetMaintenance(ctx, pool.Pool, false, ""); err != nil {
		t.Fatal(err)
	}
	if got := readyzStatus(t, pool); got != http.StatusOK {
		t.Fatalf("readyz after maintenance = %d, want 200", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd hdms-backend && go test -tags=integration ./test/integration/ -run TestReadyzReportsMaintenance -count=1`
Expected: FAIL — `readyz during maintenance = 200, type "", Retry-After ""; want 503, …/maintenance, 15`.

- [ ] **Step 3: Write minimal implementation**

In `hdms-backend/internal/apiserver/maintenance.go`, replace the two lines that write the 503 inside `MaintenanceGate`:

```go
			w.Header().Set("Retry-After", "15")
			httpx.WriteProblem(w, r, httpx.NewProblem("maintenance", "Under maintenance", http.StatusServiceUnavailable))
```

with:

```go
			writeMaintenance(w, r)
```

and append to the same file:

```go
// writeMaintenance is the one maintenance answer, from the gate and from
// /v1/readyz alike. Clients key their notice on the problem type and ask
// /v1/readyz again after Retry-After.
func writeMaintenance(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Retry-After", "15")
	httpx.WriteProblem(w, r, httpx.NewProblem("maintenance", "Under maintenance", http.StatusServiceUnavailable))
}
```

In `hdms-backend/internal/apiserver/server.go`, replace `GetReadyz` (and its comment) with:

```go
// GetReadyz is ready when the database answers, carries this build's schema
// and no restore holds maintenance mode. A database that is down, empty or
// mid-restore is "not ready"; /v1/healthz still reports the process itself.
// During a restore every client shows its maintenance notice and asks here
// every 15 seconds: a 200 is how it learns the restore is over.
func (s *Server) GetReadyz(w http.ResponseWriter, r *http.Request) {
	if err := s.pool.HealthCheck(r.Context()); err != nil {
		httpx.WriteProblem(w, r, httpx.NewProblem("not-ready", "Dependency unavailable", http.StatusServiceUnavailable))
		return
	}
	if err := db.SchemaCurrent(r.Context(), s.pool.Pool); err != nil {
		httpx.WriteProblem(w, r, httpx.NewProblem("not-ready", "Database schema not current", http.StatusServiceUnavailable))
		return
	}
	m, err := backup.GetMaintenance(r.Context(), s.pool.Pool)
	if err != nil {
		httpx.WriteProblem(w, r, httpx.NewProblem("not-ready", "Dependency unavailable", http.StatusServiceUnavailable))
		return
	}
	if m.On {
		writeMaintenance(w, r)
		return
	}
	writeJSON(w, http.StatusOK, gen.HealthStatus{Status: gen.HealthStatusStatusOk})
}
```

(`backup` is already imported in `server.go`.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd hdms-backend && go test -tags=integration ./test/integration/ -run 'TestReadyz|TestMaintenance|TestSchemaCurrent' -count=1`
Expected: PASS (the gate test still sees `Retry-After: 15` and `…/maintenance`).

- [ ] **Step 5: Update the contract and regenerate**

In `hdms-backend/api/openapi.yaml`, under `/readyz` → `responses` → `"503"`, replace

```yaml
          description: A dependency is unavailable.
```

with

```yaml
          description: >
            Not ready. Problem type `not-ready` when the database is down or
            its schema is not current; `maintenance` (with `Retry-After: 15`)
            while a restore holds maintenance mode. Clients showing a
            maintenance notice poll here and clear it on the first 200.
```

Run: `task generate:backend && task generate:frontend` (from the repository root), then `cd hdms-backend && go build ./... && gofmt -l . && golangci-lint run ./...`
Expected: build succeeds, `gofmt -l .` prints nothing, `0 issues.`

- [ ] **Step 6: Commit**

```bash
git add hdms-backend/internal/apiserver/maintenance.go hdms-backend/internal/apiserver/server.go \
  hdms-backend/api/openapi.yaml hdms-backend/internal/platform/httpx/gen/api.gen.go \
  hdms-backend/test/integration/readiness_test.go hdms-frontend/packages/api-client/src/gen
git commit -m "feat(api): /v1/readyz answers maintenance while a restore holds it"
```

---

### Task 2: Shared maintenance store in `@hdms/ui`

**Files:**
- Create: `hdms-frontend/packages/ui/src/maintenance.ts`
- Modify: `hdms-frontend/packages/ui/src/index.ts`
- Test: `hdms-frontend/packages/ui/src/maintenance.test.tsx`

**Interfaces:**
- Consumes: `getReadyz` and `client` from `@hdms/api-client` (Task 1's contract).
- Produces (all exported from `@hdms/ui`):
  - `MAINTENANCE_RECHECK_MS: number` (15000)
  - `reportMaintenance(): void` — idempotent; starts the 15 s re-check loop
  - `clearMaintenance(): void`
  - `isUnderMaintenance(): boolean`
  - `subscribeMaintenance(listener: () => void): () => void`
  - `useMaintenance(onEnd?: () => void): boolean` — `onEnd` runs once each time maintenance ends
  - `isMaintenanceResponse(response: Response): Promise<boolean>` — does not consume the body
  - `installMaintenanceInterceptor(): void` — idempotent; for apps calling the generated client directly
  - `resetMaintenanceForTesting(probe?: () => Promise<boolean>): void`

- [ ] **Step 1: Write the failing test**

Create `hdms-frontend/packages/ui/src/maintenance.test.tsx`:

```tsx
import "@testing-library/jest-dom/vitest";
import { act, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { client } from "@hdms/api-client";
import {
  MAINTENANCE_RECHECK_MS,
  clearMaintenance,
  installMaintenanceInterceptor,
  isMaintenanceResponse,
  isUnderMaintenance,
  reportMaintenance,
  resetMaintenanceForTesting,
  useMaintenance,
} from "./maintenance";

vi.mock("@hdms/api-client", () => ({
  client: { interceptors: { response: { use: vi.fn() } } },
  getReadyz: vi.fn(),
}));

function problem(status: number, type: string): Response {
  return new Response(
    JSON.stringify({ type: `https://hdms.hito.local/errors/${type}`, title: "x", status }),
    { status, headers: { "Content-Type": "application/problem+json" } },
  );
}

describe("isMaintenanceResponse", () => {
  it("recognises only a 503 whose problem type is maintenance", async () => {
    expect(await isMaintenanceResponse(problem(503, "maintenance"))).toBe(true);
    expect(await isMaintenanceResponse(problem(503, "not-ready"))).toBe(false);
    expect(await isMaintenanceResponse(problem(500, "maintenance"))).toBe(false);
    expect(await isMaintenanceResponse(new Response("<html>bad gateway</html>", { status: 503 }))).toBe(false);
  });

  it("leaves the body readable for the caller", async () => {
    const res = problem(503, "maintenance");
    await isMaintenanceResponse(res);
    expect(((await res.json()) as { type: string }).type).toMatch(/maintenance$/);
  });
});

describe("maintenance state", () => {
  let ready: boolean | Error = false;
  const probe = vi.fn(async () => {
    if (ready instanceof Error) throw ready;
    return ready;
  });

  beforeEach(() => {
    vi.useFakeTimers();
    ready = false;
    probe.mockClear();
    resetMaintenanceForTesting(probe);
  });

  afterEach(() => {
    resetMaintenanceForTesting();
    vi.useRealTimers();
  });

  it("re-checks every 15 seconds and ends only when readyz is ready", async () => {
    reportMaintenance();
    reportMaintenance();
    expect(isUnderMaintenance()).toBe(true);

    await vi.advanceTimersByTimeAsync(MAINTENANCE_RECHECK_MS - 1);
    expect(probe).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(1);
    expect(probe).toHaveBeenCalledTimes(1);
    expect(isUnderMaintenance()).toBe(true);

    ready = new Error("network down");
    await vi.advanceTimersByTimeAsync(MAINTENANCE_RECHECK_MS);
    expect(probe).toHaveBeenCalledTimes(2);
    expect(isUnderMaintenance()).toBe(true);

    ready = true;
    await vi.advanceTimersByTimeAsync(MAINTENANCE_RECHECK_MS);
    expect(isUnderMaintenance()).toBe(false);

    // Over means over: no more probing until the next report.
    await vi.advanceTimersByTimeAsync(MAINTENANCE_RECHECK_MS * 3);
    expect(probe).toHaveBeenCalledTimes(3);
  });

  it("useMaintenance re-renders and runs onEnd once when maintenance ends", () => {
    const onEnd = vi.fn();
    function Flag() {
      return <p>{useMaintenance(onEnd) ? "on" : "off"}</p>;
    }
    render(<Flag />);
    expect(screen.getByText("off")).toBeInTheDocument();

    act(() => reportMaintenance());
    expect(screen.getByText("on")).toBeInTheDocument();
    expect(onEnd).not.toHaveBeenCalled();

    act(() => clearMaintenance());
    expect(screen.getByText("off")).toBeInTheDocument();
    expect(onEnd).toHaveBeenCalledTimes(1);
  });

  it("the interceptor reports a maintenance response and passes every response through", async () => {
    installMaintenanceInterceptor();
    installMaintenanceInterceptor();
    const use = vi.mocked(client.interceptors.response.use);
    expect(use).toHaveBeenCalledTimes(1);
    const interceptor = use.mock.calls[0][0] as unknown as (r: Response) => Promise<Response>;

    const ok = new Response("{}", { status: 200 });
    expect(await interceptor(ok)).toBe(ok);
    expect(isUnderMaintenance()).toBe(false);

    const down = problem(503, "maintenance");
    expect(await interceptor(down)).toBe(down);
    expect(isUnderMaintenance()).toBe(true);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd hdms-frontend && pnpm --filter @hdms/ui test`
Expected: FAIL — `Failed to resolve import "./maintenance"`.

- [ ] **Step 3: Write minimal implementation**

Create `hdms-frontend/packages/ui/src/maintenance.ts`:

```ts
import * as React from "react";
import { client, getReadyz } from "@hdms/api-client";

// While a restore runs, the API answers 503 with problem type "maintenance"
// (hdms-backend/internal/apiserver/maintenance.go). Every app shows a notice
// instead of its usual error or offline screen and asks /v1/readyz every 15
// seconds; a 200 there means the restore is over. One store for all apps, so
// the kiosk, staff and admin cannot disagree about what "over" means.
export const MAINTENANCE_RECHECK_MS = 15_000;

type Listener = () => void;

const listeners = new Set<Listener>();
let active = false;
let timer: ReturnType<typeof setTimeout> | null = null;

async function readyzIsReady(): Promise<boolean> {
  const { response } = await getReadyz();
  return response?.ok ?? false;
}

let probe: () => Promise<boolean> = readyzIsReady;

function notify(): void {
  for (const listener of listeners) listener();
}

// Anything but a 200 — "not-ready" while the swap terminates connections, a
// network error, the maintenance answer itself — keeps the notice up.
function scheduleRecheck(): void {
  if (timer !== null) return;
  timer = setTimeout(async () => {
    timer = null;
    if (!active) return;
    let ready = false;
    try {
      ready = await probe();
    } catch {
      ready = false;
    }
    if (ready) clearMaintenance();
    else scheduleRecheck();
  }, MAINTENANCE_RECHECK_MS);
}

export function reportMaintenance(): void {
  const wasActive = active;
  active = true;
  scheduleRecheck();
  if (!wasActive) notify();
}

export function clearMaintenance(): void {
  if (timer !== null) {
    clearTimeout(timer);
    timer = null;
  }
  if (!active) return;
  active = false;
  notify();
}

export function isUnderMaintenance(): boolean {
  return active;
}

export function subscribeMaintenance(listener: Listener): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

/** True while maintenance is on. `onEnd` runs once each time it ends — apps refetch there. */
export function useMaintenance(onEnd?: () => void): boolean {
  const on = React.useSyncExternalStore(subscribeMaintenance, isUnderMaintenance, () => false);
  const onEndRef = React.useRef(onEnd);
  React.useLayoutEffect(() => {
    onEndRef.current = onEnd;
  });
  const wasOn = React.useRef(on);
  React.useEffect(() => {
    if (wasOn.current && !on) onEndRef.current?.();
    wasOn.current = on;
  }, [on]);
  return on;
}

/** A 503 whose problem type is maintenance. Reads a clone; the body stays unread. */
export async function isMaintenanceResponse(response: Response): Promise<boolean> {
  if (response.status !== 503) return false;
  try {
    const body = (await response.clone().json()) as { type?: unknown };
    return typeof body.type === "string" && body.type.endsWith("/maintenance");
  } catch {
    return false;
  }
}

let interceptorInstalled = false;

/** For apps that call the API through the generated client (admin, staff). */
export function installMaintenanceInterceptor(): void {
  if (interceptorInstalled) return;
  interceptorInstalled = true;
  client.interceptors.response.use(async (response) => {
    if (await isMaintenanceResponse(response)) reportMaintenance();
    return response;
  });
}

export function resetMaintenanceForTesting(nextProbe: () => Promise<boolean> = readyzIsReady): void {
  if (timer !== null) clearTimeout(timer);
  timer = null;
  active = false;
  probe = nextProbe;
  notify();
}
```

Append to `hdms-frontend/packages/ui/src/index.ts`:

```ts
export {
  MAINTENANCE_RECHECK_MS,
  clearMaintenance,
  installMaintenanceInterceptor,
  isMaintenanceResponse,
  isUnderMaintenance,
  reportMaintenance,
  resetMaintenanceForTesting,
  subscribeMaintenance,
  useMaintenance,
} from "./maintenance";
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd hdms-frontend && pnpm --filter @hdms/ui test && pnpm --filter @hdms/ui lint`
Expected: PASS, lint clean.

- [ ] **Step 5: Commit**

```bash
git add hdms-frontend/packages/ui/src/maintenance.ts hdms-frontend/packages/ui/src/maintenance.test.tsx hdms-frontend/packages/ui/src/index.ts
git commit -m "feat(ui): shared maintenance store that re-checks /v1/readyz"
```

---

### Task 3: Kiosk maintenance screen

**Files:**
- Modify: `hdms-frontend/apps/kiosk/src/lib/api.ts`, `hdms-frontend/apps/kiosk/src/machine/use-kiosk-session.ts`, `hdms-frontend/apps/kiosk/src/routes/index.tsx`, `hdms-frontend/apps/kiosk/src/screens/index.ts`, `hdms-frontend/apps/kiosk/src/i18n/en.ts`, `hdms-frontend/apps/kiosk/src/i18n/ja.ts`
- Create: `hdms-frontend/apps/kiosk/src/screens/maintenance-screen.tsx`
- Test: `hdms-frontend/apps/kiosk/src/lib/api.test.ts`, `hdms-frontend/apps/kiosk/src/lib/replay-engine.test.ts`, `hdms-frontend/apps/kiosk/src/screens/maintenance-screen.test.tsx`, `hdms-frontend/apps/kiosk/src/routes/maintenance.test.tsx`

**Interfaces:**
- Consumes: `isMaintenanceResponse`, `reportMaintenance`, `clearMaintenance`, `isUnderMaintenance`, `subscribeMaintenance`, `useMaintenance`, `resetMaintenanceForTesting` from `@hdms/ui` (Task 2).
- Produces: `MaintenanceScreen({ kioskName?, supportCode?, onOpenDiagnostics? })`; catalogue keys `maintenance.badge|title|subtitle|notice`.

- [ ] **Step 1: Check the Japanese glyphs before touching the catalogue**

The kiosk's Japanese font is a subset of the glyphs its catalogue used when it was generated (`packages/ui/src/tokens.css`). Run, from `hdms-frontend/apps/kiosk`:

```bash
node -e '
const fs = require("fs");
const have = new Set(fs.readFileSync("src/i18n/ja.ts", "utf8"));
const added = ["メンテナンス中", "ただいまメンテナンス中です", "HDMSはメンテナンス中です。まもなく自動的に再開します。", "状態を自動的に確認しています。"].join("");
const missing = [...new Set(added)].filter((c) => !have.has(c));
console.log(missing.length ? "MISSING: " + missing.join("") : "all glyphs already in the catalogue");'
```

Expected: `all glyphs already in the catalogue`. If anything is missing, reword with glyphs that are present; do not ship a kanji the subset lacks.

- [ ] **Step 2: Write the failing tests**

In `hdms-frontend/apps/kiosk/src/lib/api.test.ts`, add to the imports:

```ts
import { isUnderMaintenance, resetMaintenanceForTesting } from "@hdms/ui";
```

add `resetMaintenanceForTesting(async () => false);` as the last line of the existing `beforeEach`, and add this test inside the `describe`:

```ts
  it("aMaintenance503IsNeitherRetriedNorCountedAsOffline", async () => {
    let callCount = 0;
    vi.spyOn(globalThis, "fetch").mockImplementation(async () => {
      callCount += 1;
      return new Response(
        JSON.stringify({ type: "https://hdms.hito.local/errors/maintenance", title: "Under maintenance", status: 503 }),
        { status: 503, headers: { "Content-Type": "application/problem+json", "Retry-After": "15" } },
      );
    });

    const first = await resilientFetch("https://api.test/v1/sessions/123/scan");
    const second = await resilientFetch("https://api.test/v1/sessions/123/scan");

    expect(first.status).toBe(503);
    expect(second.status).toBe(503);
    // No retries, and two of them would have flipped the kiosk offline.
    expect(callCount).toBe(2);
    expect(isKioskOffline()).toBe(false);
    expect(isUnderMaintenance()).toBe(true);
  });
```

In `hdms-frontend/apps/kiosk/src/lib/replay-engine.test.ts`, add inside the `describe` (this one already passes; it pins the 5xx branch so a later change cannot quarantine scans during a restore):

```ts
  it("holdsQueuedItemsWhileTheServerIsUnderMaintenance", async () => {
    await enqueueQueueItem({
      kioskId: KIOSK_ID,
      request: {
        method: "POST",
        url: "/v1/sessions/return",
        body: JSON.stringify({ deviceId: "DEV-M", action: "return" }),
      },
    });
    const mockFetch = vi.fn(async () =>
      new Response(
        JSON.stringify({ type: "https://hdms.hito.local/errors/maintenance", title: "Under maintenance", status: 503 }),
        { status: 503, headers: { "Content-Type": "application/problem+json" } },
      ),
    );

    const result = await replayQueue({ kioskId: KIOSK_ID, fetchFn: mockFetch as any });

    expect(result.quarantined).toBe(0);
    expect(result.succeeded).toBe(0);
    expect(result.remaining).toBe(1);
    expect(await getPendingQueueItems(KIOSK_ID)).toHaveLength(1);
  });
```

Create `hdms-frontend/apps/kiosk/src/screens/maintenance-screen.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { axe } from "vitest-axe";
import { LocaleProvider } from "@hdms/i18n";
import { MaintenanceScreen } from "./maintenance-screen";

describe("MaintenanceScreen", () => {
  it("tells staff to use the paper register and that it comes back by itself", async () => {
    const { container } = render(
      <LocaleProvider locale="en">
        <MaintenanceScreen kioskName="Ward 3" supportCode="KIOSK-W3" />
      </LocaleProvider>,
    );

    expect(screen.getByTestId("maintenance-title")).toHaveTextContent("Under maintenance");
    expect(screen.getByTestId("maintenance-subtitle")).toHaveTextContent(/resume by itself/i);
    expect(screen.getByTestId("maintenance-paper-instruction")).toHaveTextContent(/paper register/i);
    expect(await axe(container)).toHaveNoViolations();
  });

  it("shows no technical strings", () => {
    const { container } = render(<MaintenanceScreen />);
    const text = container.textContent ?? "";
    expect(text).not.toMatch(/503|maintenance_|restore_|https?:\/\//i);
  });

  it("speaks Japanese", () => {
    render(
      <LocaleProvider locale="ja">
        <MaintenanceScreen />
      </LocaleProvider>,
    );
    expect(screen.getByTestId("maintenance-title")).toHaveTextContent("ただいまメンテナンス中です");
  });
});
```

Create `hdms-frontend/apps/kiosk/src/routes/maintenance.test.tsx`:

```tsx
import { act, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { clearMaintenance, reportMaintenance, resetMaintenanceForTesting } from "@hdms/ui";
import { KioskApp } from "./index";
import { getSessionId, setKioskConfig, setSessionId } from "@/lib/kiosk-config";
import { resetConnectivityForTesting } from "@/lib/connectivity";

describe("kiosk under maintenance", () => {
  beforeEach(() => {
    localStorage.clear();
    resetConnectivityForTesting(false);
    resetMaintenanceForTesting(async () => false);
    setKioskConfig({ kioskId: "k1", kioskName: "Ward 3", token: "tok", defaultLocale: "en" });
  });

  afterEach(() => {
    resetMaintenanceForTesting();
    resetConnectivityForTesting(false);
  });

  it("replaces the kiosk with the notice, drops the open session, and returns to idle by itself", async () => {
    render(<KioskApp />);
    expect(screen.getByTestId("start-scanning-button")).toBeInTheDocument();
    setSessionId("session-open");

    act(() => reportMaintenance());

    expect(await screen.findByTestId("maintenance-screen")).toBeInTheDocument();
    expect(screen.queryByTestId("start-scanning-button")).toBeNull();
    expect(getSessionId()).toBeNull();

    act(() => clearMaintenance());
    expect(await screen.findByTestId("start-scanning-button")).toBeInTheDocument();
  });

  it("shows maintenance, not offline, when both apply", async () => {
    render(<KioskApp />);
    act(() => {
      resetConnectivityForTesting(true);
      reportMaintenance();
    });
    expect(await screen.findByTestId("maintenance-screen")).toBeInTheDocument();
    expect(screen.queryByTestId("offline-screen")).toBeNull();
  });
});
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `cd hdms-frontend && pnpm --filter kiosk test`
Expected: FAIL — `aMaintenance503IsNeitherRetriedNorCountedAsOffline` (callCount 8, kiosk offline), `Failed to resolve import "./maintenance-screen"`, and `routes/maintenance.test.tsx` cannot find `maintenance-screen`. `holdsQueuedItemsWhileTheServerIsUnderMaintenance` passes.

- [ ] **Step 4: Write minimal implementation**

`hdms-frontend/apps/kiosk/src/i18n/en.ts` — add after the `offline` block:

```ts
  maintenance: {
    badge: "Maintenance",
    title: "Under maintenance",
    subtitle: "HDMS is under maintenance. This kiosk will resume by itself shortly.",
    notice: "Checking automatically for the end of maintenance.",
  },
```

`hdms-frontend/apps/kiosk/src/i18n/ja.ts` — add after the `offline` block:

```ts
  maintenance: {
    badge: "メンテナンス中",
    title: "ただいまメンテナンス中です",
    subtitle: "HDMSはメンテナンス中です。まもなく自動的に再開します。",
    notice: "状態を自動的に確認しています。",
  },
```

Create `hdms-frontend/apps/kiosk/src/screens/maintenance-screen.tsx`:

```tsx
import { BookOpen, Wrench } from "lucide-react";
import { useTranslator } from "@/i18n";
import { ScreenFrame } from "./screen-frame";

export interface MaintenanceScreenProps {
  kioskName?: string;
  supportCode?: string | null;
  onOpenDiagnostics?: () => void;
}

// A restore is rewinding the data. Nothing scanned here would count, so the
// kiosk takes no scans and sends people to the paper register — the same
// fallback as offline, but without the "reconnecting" story, because the
// network is fine. It comes back by itself (packages/ui maintenance store).
export function MaintenanceScreen({
  kioskName = "HDMS Kiosk",
  supportCode = "KIOSK-01",
  onOpenDiagnostics,
}: MaintenanceScreenProps) {
  const t = useTranslator();

  return (
    <ScreenFrame
      kioskName={kioskName}
      supportCode={supportCode}
      scannerReady={false}
      scannerFresh={false}
      onOpenDiagnostics={onOpenDiagnostics}
    >
      <div
        data-testid="maintenance-screen"
        className="flex flex-col items-center justify-between h-full w-full max-w-3xl space-y-8 py-6 text-center"
      >
        <div className="space-y-4 flex flex-col items-center">
          <div className="p-5 rounded-full bg-muted text-muted-foreground shadow-md">
            <Wrench className="size-16 stroke-[2.5]" aria-hidden="true" />
          </div>
          <span className="font-mono text-xs font-bold uppercase tracking-wider px-3.5 py-1 rounded-full bg-muted text-muted-foreground">
            {t("maintenance.badge")}
          </span>
          <h2 data-testid="maintenance-title" className="text-kiosk-prompt text-foreground leading-tight">
            {t("maintenance.title")}
          </h2>
          <p data-testid="maintenance-subtitle" className="text-kiosk-body text-muted-foreground max-w-lg">
            {t("maintenance.subtitle")}
          </p>
        </div>

        <div className="w-full max-w-xl rounded-2xl border-2 border-primary/30 bg-primary/5 p-6 md:p-8 shadow-sm text-left">
          <div className="flex items-start gap-4">
            <div className="p-3 rounded-xl bg-primary/10 text-primary shrink-0 mt-0.5">
              <BookOpen className="size-7" aria-hidden="true" />
            </div>
            <div className="space-y-2 flex-1">
              <h3 className="text-lg font-bold text-foreground">{t("offline.paperFallbackTitle")}</h3>
              <p
                data-testid="maintenance-paper-instruction"
                className="text-base text-foreground/80 leading-relaxed font-medium"
              >
                {t("offline.paperFallbackInstruction")}
              </p>
            </div>
          </div>
        </div>

        <div className="w-full max-w-lg pt-2 text-xs text-muted-foreground">
          <p>{t("maintenance.notice")}</p>
        </div>
      </div>
    </ScreenFrame>
  );
}
```

Append to `hdms-frontend/apps/kiosk/src/screens/index.ts`:

```ts
export * from "./maintenance-screen";
```

In `hdms-frontend/apps/kiosk/src/lib/api.ts`, add the import:

```ts
import { isMaintenanceResponse, reportMaintenance } from "@hdms/ui";
```

and in `resilientFetch`, directly after the block that clears the timeout and removes the abort listener following `await globalThis.fetch(...)`, and before `// Check if status is 5xx or 408/429`, insert:

```ts
      // A restore is running: not worth retrying and not a connectivity
      // failure. The maintenance screen takes over until /v1/readyz is 200.
      if (await isMaintenanceResponse(response)) {
        reportMaintenance();
        return response;
      }
```

In `hdms-frontend/apps/kiosk/src/machine/use-kiosk-session.ts`:

- change `import { resetScanSequence } from "../lib/api";` to `import { abortActiveSessionRequests, resetScanSequence } from "../lib/api";`
- add `import { isUnderMaintenance, subscribeMaintenance } from "@hdms/ui";`
- after the `// Resync session on reconnect from offline` effect, add:

```ts
  // A restore is about to rewind the data: drop whatever session is open.
  // The server-side session expires on its own, and nothing is queued.
  React.useEffect(() => {
    return subscribeMaintenance(() => {
      if (!isUnderMaintenance()) return;
      abortActiveSessionRequests();
      clearSessionId();
      actor.send({ type: "RESET" });
    });
  }, [actor]);
```

- in `handleScan`, change the first line `if (!isScanningRef.current) return;` to `if (!isScanningRef.current || isUnderMaintenance()) return;`
- in the `catch` of `handleScan` and of `handleReturnLoan`, insert as the first statement after the existing abort check:

```ts
        // The maintenance screen is already up; this is not a refusal.
        if (isUnderMaintenance()) return;
```

In `hdms-frontend/apps/kiosk/src/routes/index.tsx`:

- add `import { useMaintenance } from "@hdms/ui";` and `import { MaintenanceScreen } from "@/screens/maintenance-screen";`
- after the `useKioskSession()` destructuring, add `const underMaintenance = useMaintenance();`
- replace the opening of the screen chain

```tsx
      {/* 1. Offline Mode Display */}
      {isOffline ? (
```

with

```tsx
      {/* 0. Maintenance outranks everything, offline included */}
      {underMaintenance ? (
        <MaintenanceScreen
          kioskName={kioskName}
          supportCode={context.supportCode}
          onOpenDiagnostics={() => setIsDiagnosticsOpen(true)}
        />
      ) : isOffline ? (
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd hdms-frontend && pnpm --filter kiosk test && pnpm --filter kiosk lint && pnpm --filter kiosk build`
Expected: PASS (all kiosk tests, including `no-literals`), lint clean, build succeeds.

- [ ] **Step 6: Commit**

```bash
git add hdms-frontend/apps/kiosk/src
git commit -m "feat(kiosk): maintenance screen instead of offline during a restore"
```

---

### Task 4: Staff and admin maintenance notices

**Files:**
- Create: `hdms-frontend/apps/staff/src/components/maintenance-notice.tsx`, `hdms-frontend/apps/admin/src/components/maintenance-notice.tsx`
- Modify: `hdms-frontend/apps/staff/src/App.tsx`, `hdms-frontend/apps/staff/src/routes/root.tsx`, `hdms-frontend/apps/staff/src/routes/authenticated.tsx`, `hdms-frontend/apps/staff/src/i18n/en.ts`, `hdms-frontend/apps/staff/src/i18n/ja.ts`, `hdms-frontend/apps/admin/src/App.tsx`, `hdms-frontend/apps/admin/src/routes/root.tsx`, `hdms-frontend/apps/admin/src/i18n/ja.ts`, `hdms-frontend/apps/admin/src/i18n/en.ts`
- Test: `hdms-frontend/apps/staff/src/__tests__/maintenance.test.tsx`, `hdms-frontend/apps/admin/src/__tests__/maintenance-notice.test.tsx`

**Interfaces:**
- Consumes: `installMaintenanceInterceptor`, `useMaintenance`, `isUnderMaintenance`, `reportMaintenance`, `clearMaintenance`, `resetMaintenanceForTesting` from `@hdms/ui` (Task 2).
- Produces: `data-testid="maintenance-notice"` in both apps; catalogue keys `maintenance.title|body|recheck` (staff) and `maintenance.title|body|recheck|openBackups` (admin).

- [ ] **Step 1: Write the failing tests**

Create `hdms-frontend/apps/staff/src/__tests__/maintenance.test.tsx`:

```tsx
import { act, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, RouterProvider } from "@tanstack/react-router";
import { LocaleProvider } from "@hdms/i18n";
import { clearMaintenance, reportMaintenance, resetMaintenanceForTesting } from "@hdms/ui";
import * as apiClient from "@hdms/api-client";
import { createStaffRouter } from "@/router";

const maintenanceProblem = {
  type: "https://hdms.hito.local/errors/maintenance",
  title: "Under maintenance",
  status: 503,
};

describe("staff app under maintenance", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    resetMaintenanceForTesting(async () => false);
  });

  afterEach(() => {
    resetMaintenanceForTesting();
    vi.restoreAllMocks();
  });

  it("shows the notice instead of sending a signed-in member to login, then reloads when it ends", async () => {
    const getMe = vi.spyOn(apiClient, "getStaffMe").mockImplementation(async () => {
      // What installMaintenanceInterceptor does for a real 503 maintenance.
      reportMaintenance();
      return { data: undefined, error: maintenanceProblem } as any;
    });
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const testRouter = createStaffRouter(createMemoryHistory({ initialEntries: ["/devices"] }), queryClient);

    render(
      <LocaleProvider locale="en">
        <QueryClientProvider client={queryClient}>
          <RouterProvider router={testRouter} />
        </QueryClientProvider>
      </LocaleProvider>,
    );

    expect(await screen.findByTestId("maintenance-notice")).toHaveTextContent("HDMS is under maintenance");
    expect(testRouter.state.location.pathname).toBe("/devices");

    const callsDuringMaintenance = getMe.mock.calls.length;
    getMe.mockResolvedValue({
      data: undefined,
      error: { type: "https://hdms.hito.local/errors/unauthorized", status: 401 },
    } as any);
    act(() => clearMaintenance());

    await waitFor(() => expect(screen.queryByTestId("maintenance-notice")).toBeNull());
    // The guard ran again on its own: the router was invalidated.
    await waitFor(() => expect(getMe.mock.calls.length).toBeGreaterThan(callsDuringMaintenance));
  });
});
```

Create `hdms-frontend/apps/admin/src/__tests__/maintenance-notice.test.tsx`:

```tsx
import { act, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRouter, RouterProvider } from "@tanstack/react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Admin } from "@hdms/api-client";
import {
  clearMaintenance,
  installMaintenanceInterceptor,
  reportMaintenance,
  resetMaintenanceForTesting,
} from "@hdms/ui";
import { currentAdminQueryKey } from "@/lib/auth";
import { router } from "@/router";

const admin: Admin = {
  id: "admin-1",
  email: "admin@hito.local",
  fullName: "System Admin",
  role: "admin",
  status: "active",
  locale: "en",
};

function renderAt(path: string) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  queryClient.setQueryData(currentAdminQueryKey, admin);
  const testRouter = createRouter({
    routeTree: router.routeTree,
    history: createMemoryHistory({ initialEntries: [path] }),
    context: { queryClient },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={testRouter} />
    </QueryClientProvider>,
  );
  return { queryClient, testRouter };
}

function maintenance503(): Response {
  return new Response(
    JSON.stringify({ type: "https://hdms.hito.local/errors/maintenance", title: "Under maintenance", status: 503 }),
    { status: 503, headers: { "Content-Type": "application/problem+json", "Retry-After": "15" } },
  );
}

describe("admin console under maintenance", () => {
  beforeEach(() => {
    resetMaintenanceForTesting(async () => false);
  });

  afterEach(() => {
    resetMaintenanceForTesting();
    vi.restoreAllMocks();
  });

  it("a 503 maintenance from any call replaces the page with the notice and a way to Backups", async () => {
    installMaintenanceInterceptor();
    vi.spyOn(globalThis, "fetch").mockImplementation(async () => maintenance503());
    renderAt("/dashboard");

    expect(await screen.findByTestId("maintenance-notice")).toHaveTextContent("HDMS is under maintenance");
    expect(screen.getByRole("link", { name: "Open Backups" })).toHaveAttribute("href", "/backups");
  });

  it("leaves the Backups page usable during maintenance", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async () => new Response("{}", { status: 500 }));
    const { testRouter } = renderAt("/backups");
    act(() => reportMaintenance());

    await waitFor(() => expect(testRouter.state.status).toBe("idle"));
    expect(screen.queryByTestId("maintenance-notice")).toBeNull();
  });

  it("refetches everything when maintenance ends", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async () => new Response("{}", { status: 500 }));
    const { queryClient } = renderAt("/dashboard");
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");

    act(() => reportMaintenance());
    expect(await screen.findByTestId("maintenance-notice")).toBeInTheDocument();

    act(() => clearMaintenance());
    await waitFor(() => expect(screen.queryByTestId("maintenance-notice")).toBeNull());
    expect(invalidate).toHaveBeenCalled();
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd hdms-frontend && pnpm --filter staff test -- maintenance && pnpm --filter admin test -- maintenance-notice`
Expected: FAIL — `Unable to find an element by: [data-testid="maintenance-notice"]` in both apps.

- [ ] **Step 3: Write minimal implementation**

**Staff.** `hdms-frontend/apps/staff/src/i18n/en.ts` — add after the `environment` block:

```ts
  maintenance: {
    title: "HDMS is under maintenance",
    body: "HDMS data is being restored from a backup. Please try again in a few minutes.",
    recheck: "This page comes back by itself when maintenance ends.",
  },
```

`hdms-frontend/apps/staff/src/i18n/ja.ts` — add after the `environment` block:

```ts
  maintenance: {
    title: "HDMS はメンテナンス中です",
    body: "バックアップからデータを復旧しています。数分後にもう一度お試しください。",
    recheck: "メンテナンスが終わると、このページは自動的に元に戻ります。",
  },
```

Create `hdms-frontend/apps/staff/src/components/maintenance-notice.tsx`:

```tsx
import { Wrench } from "lucide-react";
import { useTranslator } from "@/i18n";

export function MaintenanceNotice() {
  const t = useTranslator();
  return (
    <main
      role="status"
      data-testid="maintenance-notice"
      className="mx-auto flex min-h-dvh max-w-md flex-col items-center justify-center gap-4 px-6 text-center"
    >
      <Wrench className="size-12 text-muted-foreground" aria-hidden="true" />
      <h1 className="text-2xl font-bold">{t("maintenance.title")}</h1>
      <p className="text-muted-foreground">{t("maintenance.body")}</p>
      <p className="text-sm text-muted-foreground">{t("maintenance.recheck")}</p>
    </main>
  );
}
```

Replace `hdms-frontend/apps/staff/src/routes/root.tsx` with:

```tsx
import type { QueryClient } from "@tanstack/react-query";
import { useQueryClient } from "@tanstack/react-query";
import { Outlet, createRootRouteWithContext, useRouter } from "@tanstack/react-router";
import { EnvironmentBanner, useMaintenance } from "@hdms/ui";
import { MaintenanceNotice } from "@/components/maintenance-notice";
import { useTranslator } from "@/i18n";

export interface RouterContext {
  queryClient: QueryClient;
}

function RootComponent() {
  const t = useTranslator();
  const router = useRouter();
  const queryClient = useQueryClient();
  // When the restore is over, load everything again: the data may be a
  // different day's, and the auth guard has to run once more.
  const underMaintenance = useMaintenance(() => {
    void queryClient.invalidateQueries();
    void router.invalidate();
  });
  return (
    <>
      <EnvironmentBanner
        labels={{ development: t("environment.development"), staging: t("environment.staging") }}
      />
      <div className="min-h-dvh bg-background text-foreground">
        {underMaintenance ? <MaintenanceNotice /> : <Outlet />}
      </div>
    </>
  );
}

export const rootRoute = createRootRouteWithContext<RouterContext>()({
  component: RootComponent,
});
```

In `hdms-frontend/apps/staff/src/routes/authenticated.tsx`, add `import { isUnderMaintenance } from "@hdms/ui";` and change the `catch` to:

```ts
    } catch (err) {
      if (isRedirect(err)) throw err;
      // /v1/staff/me is gated during a restore. The root shows the notice;
      // sending a signed-in member to the login page would strand them there.
      if (isUnderMaintenance()) throw err;
      throw redirect({ to: "/login" });
    }
```

In `hdms-frontend/apps/staff/src/App.tsx`, change `import { EnvironmentProvider } from "@hdms/ui";` to `import { EnvironmentProvider, installMaintenanceInterceptor } from "@hdms/ui";` and add `installMaintenanceInterceptor();` on the line after `installAuthInterceptors();`.

**Admin.** `hdms-frontend/apps/admin/src/i18n/ja.ts` — add after the `environment` block:

```ts
  maintenance: {
    title: "HDMS はメンテナンス中です",
    body: "バックアップからデータを復旧しています。終わるまで、キオスクと職員用アプリにもメンテナンス中の案内が表示されます。",
    recheck: "メンテナンスが終わると、このページは自動的に元に戻ります。",
    openBackups: "バックアップを開く",
  },
```

`hdms-frontend/apps/admin/src/i18n/en.ts` — add after the `environment` block:

```ts
  maintenance: {
    title: "HDMS is under maintenance",
    body: "HDMS data is being restored from a backup. Kiosks and staff phones show a maintenance notice until it finishes.",
    recheck: "This page comes back by itself when maintenance ends.",
    openBackups: "Open Backups",
  },
```

Create `hdms-frontend/apps/admin/src/components/maintenance-notice.tsx`:

```tsx
import { Link } from "@tanstack/react-router";
import { Wrench } from "lucide-react";
import { buttonVariants } from "@/components/ui/button";
import { useT } from "@/i18n";

export function MaintenanceNotice() {
  const t = useT();
  return (
    <main
      role="status"
      data-testid="maintenance-notice"
      className="mx-auto flex min-h-dvh max-w-lg flex-col items-center justify-center gap-4 px-6 text-center"
    >
      <Wrench className="size-12 text-muted-foreground" aria-hidden="true" />
      <h1 className="text-2xl font-bold">{t("maintenance.title")}</h1>
      <p className="text-muted-foreground">{t("maintenance.body")}</p>
      <p className="text-sm text-muted-foreground">{t("maintenance.recheck")}</p>
      <Link to="/backups" className={buttonVariants({ variant: "outline" })}>
        {t("maintenance.openBackups")}
      </Link>
    </main>
  );
}
```

Replace `hdms-frontend/apps/admin/src/routes/root.tsx` with:

```tsx
import type { QueryClient } from "@tanstack/react-query";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { createRootRouteWithContext, Outlet, useRouter, useRouterState } from "@tanstack/react-router";
import { DEFAULT_LOCALE, isLocale, LocaleProvider } from "@hdms/i18n";
import { EnvironmentBanner, useMaintenance } from "@hdms/ui";
import { MaintenanceNotice } from "@/components/maintenance-notice";
import { RouteErrorBoundary } from "@/components/states";
import { currentAdminQueryOptions } from "@/lib/auth";
import { useT } from "@/i18n";

export interface RouterContext {
  queryClient: QueryClient;
}

// Sign-in and the backup console are exempt from the API's maintenance gate,
// so an admin can still reach Backups while a restore runs.
const WORKS_DURING_MAINTENANCE = /\/(backups|login)(\/|$)/;

function AdminEnvironmentBanner() {
  const t = useT();
  return (
    <EnvironmentBanner
      labels={{ development: t("environment.development"), staging: t("environment.staging") }}
    />
  );
}

function RootComponent() {
  const { data: admin } = useQuery(currentAdminQueryOptions);
  const locale = isLocale(admin?.locale) ? admin.locale : DEFAULT_LOCALE;
  const router = useRouter();
  const queryClient = useQueryClient();
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const underMaintenance = useMaintenance(() => {
    void queryClient.invalidateQueries();
    void router.invalidate();
  });
  const showNotice = underMaintenance && !WORKS_DURING_MAINTENANCE.test(pathname);

  return (
    <LocaleProvider locale={locale}>
      <AdminEnvironmentBanner />
      <div className="min-h-dvh">
        {showNotice ? (
          <MaintenanceNotice />
        ) : (
          <RouteErrorBoundary>
            <Outlet />
          </RouteErrorBoundary>
        )}
      </div>
    </LocaleProvider>
  );
}

export const rootRoute = createRootRouteWithContext<RouterContext>()({
  component: RootComponent,
});
```

In `hdms-frontend/apps/admin/src/App.tsx`, change `import { EnvironmentProvider } from "@hdms/ui";` to `import { EnvironmentProvider, installMaintenanceInterceptor } from "@hdms/ui";` and add `installMaintenanceInterceptor();` on the line after `installAuthInterceptors();`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd hdms-frontend && pnpm --filter staff test && pnpm --filter admin test`, then `pnpm --filter staff lint && pnpm --filter admin lint && pnpm --filter staff build && pnpm --filter admin build`
Expected: all PASS (admin's `no-literals` included), lint clean, builds succeed.

- [ ] **Step 5: Commit**

```bash
git add hdms-frontend/apps/staff/src hdms-frontend/apps/admin/src
git commit -m "feat(staff,admin): maintenance notice while a restore runs"
```

---

### Task 5: Recovery app — scaffold, start, key entry and keys mismatch

**Files:**
- Create: `hdms-frontend/apps/recovery/{package.json,index.html,.oxlintrc.json,.env.example,tsconfig.json,tsconfig.app.json,tsconfig.node.json,vite.config.ts,vitest.config.ts}`
- Create: `hdms-frontend/apps/recovery/src/{index.css}`, `src/i18n/{en.ts,ja.ts,index.ts,no-literals.test.ts}`, `src/lib/{api.ts,format.ts,errors.ts,source.ts}`, `src/components/{message.tsx,page.tsx}`, `src/components/ui/{button.tsx,button-variants.ts}`, `src/screens/{start-screen.tsx,unlock-screen.tsx,mismatch-screen.tsx}`, `src/test/{setup.ts,vitest-axe.d.ts,worker.ts,fixtures.ts}`
- Test: `src/lib/api.test.ts`, `src/lib/format.test.ts`, `src/screens/start-screen.test.tsx`, `src/screens/unlock-screen.test.tsx`, `src/screens/mismatch-screen.test.tsx`
- Modify: `hdms-frontend/pnpm-lock.yaml` (via `pnpm install`)

**Interfaces:**
- Consumes: the worker's `/recovery/api/*` exactly as `hdms-backend/internal/platform/recovery/handler.go` serves it (plan 3a): errors are `{"error":"<code>"}`; `GET /status` → `{database, worker, restoreRunning}`; `GET /sources` → `{sources: Source[]}`; `POST /unlock {source, key}` → `200 {keysMatch:true}` | `409 keys_mismatch` | `422 key_typo|key_format|bundle_damaged` | `401 key_wrong` | `404 source_not_found|no_bundle` | `429 too_many_attempts` + `Retry-After`; session routes answer `401 session_required` without a session.
- Produces (later tasks import these):
  - `src/lib/api.ts`: types `LiveState`, `WorkerMode`, `SourceKind`, `Step`, `Status`, `Source`, `Snapshot`, `RestoreView`; `class RecoveryError { status: number; code: string; retryAfterSeconds?: number; get sessionLost(): boolean }`; `asRecoveryError(err: unknown): RecoveryError`; `recoveryApi.{status, sources, unlock, snapshots, restore, startRestore, undo}`; `CONFIRM_WORD = "RESTORE"`; `STEPS: Step[]`
  - `src/lib/format.ts`: `formatBytes(n)`, `formatSnapshotDate(locale, iso)`, `formatAgo(locale, iso, now)`
  - `src/lib/errors.ts`: `commonError(t, err): string`
  - `src/lib/source.ts`: `sourceTitle(t, s)`, `sourceDetail(t, s)`
  - `src/i18n/index.ts`: `useT()`, `type Translate`, `type RecoveryKey`, `catalogues`
  - `src/components/message.tsx`: `Message({ tone: "info"|"warning"|"error", id?, testId?, children })`
  - `src/components/page.tsx`: `Page({ children })`
  - `src/components/ui/button.tsx`: `Button`; `src/components/ui/button-variants.ts`: `buttonVariants`
  - `src/screens/start-screen.tsx`: `StartScreen({ onChoose(source) })`
  - `src/screens/unlock-screen.tsx`: `UnlockScreen({ source, sessionEnded, onUnlocked(), onMismatch(), onBack() })`
  - `src/screens/mismatch-screen.tsx`: `MismatchScreen({ onBack() })`
  - `src/test/worker.ts`: `fakeWorker(routes)`; `src/test/fixtures.ts`: `localSource`, `nasSource`, `usbSource`, `localWithoutKey`, `workingStatus`, `snapshots`, `view(overrides)`

- [ ] **Step 1: Create the app skeleton**

`hdms-frontend/apps/recovery/package.json`:

```json
{
  "name": "recovery",
  "private": true,
  "version": "0.0.0",
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "tsc -b && vite build",
    "lint": "oxlint",
    "preview": "vite preview",
    "test": "vitest run"
  },
  "dependencies": {
    "@hdms/i18n": "workspace:*",
    "@hdms/ui": "workspace:*",
    "class-variance-authority": "^0.7.1",
    "lucide-react": "^1.32.0",
    "react": "^19.2.8",
    "react-dom": "^19.2.8"
  },
  "devDependencies": {
    "@tailwindcss/vite": "^4.3.3",
    "@testing-library/jest-dom": "^7.0.1",
    "@testing-library/react": "^16.3.2",
    "@testing-library/user-event": "^14.6.5",
    "@types/node": "^24.13.3",
    "@types/react": "^19.2.17",
    "@types/react-dom": "^19.2.3",
    "@vitejs/plugin-react": "^6.0.4",
    "jsdom": "^30.0.1",
    "oxlint": "^1.75.0",
    "tailwindcss": "^4.3.3",
    "typescript": "~6.0.2",
    "vite": "^8.2.0",
    "vitest": "^4.1.11",
    "vitest-axe": "^0.1.0"
  }
}
```

`hdms-frontend/apps/recovery/index.html`:

```html
<!doctype html>
<html lang="ja">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <meta name="robots" content="noindex" />
    <title>HDMS Recovery</title>
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
```

`hdms-frontend/apps/recovery/.oxlintrc.json`:

```json
{
  "$schema": "./node_modules/oxlint/configuration_schema.json",
  "plugins": ["react", "typescript", "oxc"],
  "rules": {
    "react/rules-of-hooks": "error",
    "react/only-export-components": ["warn", { "allowConstantExport": true }]
  }
}
```

`hdms-frontend/apps/recovery/.env.example`:

```
# The dev Caddy that forwards /recovery/api/* to worker:8090
VITE_RECOVERY_TARGET=https://localhost:8443
```

`hdms-frontend/apps/recovery/tsconfig.json`, `tsconfig.app.json`, `tsconfig.node.json`: copy the three files from `hdms-frontend/apps/staff/`, then in `tsconfig.app.json` change `"types": ["vite/client"]` to `"types": ["vite/client", "node"]` as in `apps/admin` (the `no-literals` test imports `node:fs` and `node:path`).

`hdms-frontend/apps/recovery/vite.config.ts`:

```ts
import { existsSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

const certsDir = fileURLToPath(new URL("../../../certs", import.meta.url));
const srcDir = fileURLToPath(new URL("./src", import.meta.url));
const certFile = `${certsDir}/localhost.pem`;
const keyFile = `${certsDir}/localhost-key.pem`;
const hasCerts = existsSync(certFile) && existsSync(keyFile);

// The recovery page talks only to the worker (/recovery/api/*), never to the
// API. In dev the request goes through the dev Caddy on :8443, which forwards
// it to worker:8090 — the worker port is not published. HTTPS matters here:
// the session cookie is Secure, and over plain HTTP the browser drops it.
export default defineConfig({
  // Production serves the page under /recovery behind Caddy.
  base: process.env.VITE_BASE_PATH ?? "/",
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": srcDir,
    },
  },
  server: {
    port: 5176,
    https: hasCerts ? { cert: readFileSync(certFile), key: readFileSync(keyFile) } : undefined,
    proxy: {
      "/recovery/api": {
        target: process.env.VITE_RECOVERY_TARGET || "https://localhost:8443",
        changeOrigin: true,
        secure: false,
      },
    },
  },
});
```

`hdms-frontend/apps/recovery/vitest.config.ts`:

```ts
import { fileURLToPath } from "node:url";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

const srcDir = fileURLToPath(new URL("./src", import.meta.url));

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": srcDir,
    },
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    globals: true,
  },
});
```

`hdms-frontend/apps/recovery/src/index.css`:

```css
@import "tailwindcss";
@import "@hdms/ui/tokens.css";

body {
  @apply bg-background text-foreground;
}

:root:lang(ja) {
  line-break: strict;
  word-break: normal;
  overflow-wrap: anywhere;
}

:root:lang(en) {
  overflow-wrap: break-word;
}
```

`hdms-frontend/apps/recovery/src/test/setup.ts`:

```ts
import "@testing-library/jest-dom/vitest";
import * as matchers from "vitest-axe/matchers";
import { afterEach, expect, vi } from "vitest";
import { setDefaultLocaleFallback } from "@hdms/i18n";

expect.extend(matchers);
setDefaultLocaleFallback("en");

afterEach(() => {
  vi.restoreAllMocks();
});
```

`hdms-frontend/apps/recovery/src/test/vitest-axe.d.ts`: copy `hdms-frontend/apps/kiosk/src/test/vitest-axe.d.ts` unchanged.

Run: `cd hdms-frontend && pnpm install`
Expected: `pnpm-lock.yaml` gains the `apps/recovery` importer; no other changes.

- [ ] **Step 2: Write the catalogues**

`hdms-frontend/apps/recovery/src/i18n/en.ts`:

```ts
export const en = {
  app: {
    title: "HDMS recovery",
    subtitle: "Bring HDMS back from a backup",
    language: "日本語",
    languageAria: "Switch language",
  },
  common: {
    back: "Back",
    retry: "Try again",
    loading: "Loading…",
    unreachable: "The recovery service is not responding. Check that the server is on, then try again.",
    unexpected: "Something went wrong. Try again; if it keeps happening, show this screen to IT.",
  },
  status: {
    heading: "HDMS status",
    working: "HDMS database is working",
    workingHint: "You can still restore, for example to practise. Restoring replaces today's data.",
    empty: "HDMS database is empty",
    emptyHint: "This looks like a new installation. Restore a backup to bring your data back.",
    damaged: "HDMS database is damaged",
    damagedHint: "HDMS cannot use its database. Restore a backup to bring it back.",
    serverDown: "The database server is not running",
    serverDownHint: "Ask IT to follow the disaster recovery runbook, section \"Database server will not start\". Then check again.",
    checkAgain: "Check again",
    restoreRunning: "A restore is running. Enter the recovery key to follow it.",
  },
  sources: {
    heading: "Where are your backups?",
    local: "This server",
    localHint: "The backups kept on this server's own disk",
    folder: "Network drive or external disk",
    noKey: "No recovery key is stored here yet",
  },
  unlock: {
    heading: "Enter the recovery key",
    hint: "It is on the printed recovery sheet: 28 letters and numbers in 7 groups.",
    from: "Backups: {place}",
    label: "Recovery key",
    placeholder: "XXXX-XXXX-XXXX-XXXX-XXXX-XXXX-XXXX",
    submit: "Unlock",
    submitting: "Checking…",
    sessionEnded: "Your recovery session ended. Enter the key again to continue.",
    errors: {
      key_typo: "This key has a typing mistake. Check each group against the sheet.",
      key_format: "A recovery key has 28 letters and numbers in 7 groups.",
      key_wrong: "This key does not open the backups in this place.",
      too_many_attempts: "Too many attempts. Wait {minutes} min and try again.",
      no_bundle: "No recovery key is stored with these backups.",
      bundle_damaged: "The recovery file in this place is damaged. Try another place.",
      source_not_found: "This place is no longer available. Go back and choose again.",
    },
  },
  mismatch: {
    heading: "This server was set up with different keys",
    body: "The backups opened, but they belong to a server with other keys. Restoring here would bring back data that no one could sign in to.",
    action: "Ask IT to run install.sh --restore with this recovery key, then open this page again.",
  },
  snapshots: {
    heading: "Pick a backup",
    hint: "The newest backup is highlighted.",
    newest: "Newest",
    when: "{date} — {ago}",
    size: "Size {size}",
    choose: "Restore this backup",
    empty: "There are no backups in this place.",
    unreadable: "The backups in this place cannot be read. Check that the drive is connected, then try again.",
    lastRestore: "HDMS was restored from {date}. If that was a mistake, you can undo it.",
    undo: "Undo this restore",
  },
  confirm: {
    heading: "Confirm the restore",
    replace: "Everything recorded after {date} will be replaced.",
    kiosks: "Kiosks stop for a few minutes. Use the paper register meanwhile.",
    undoHeading: "Confirm the undo",
    undoBody: "HDMS goes back to how it was just before the restore from {date}.",
    typeLabel: "Type RESTORE to confirm",
    start: "Restore",
    startUndo: "Undo the restore",
    starting: "Starting…",
    errors: {
      database_server_down: "The database server is not running. Ask IT, then try again.",
      snapshot_not_found: "This backup is no longer there. Go back and pick another.",
      nothing_to_undo: "There is no restore to undo.",
    },
  },
  progress: {
    heading: "Restoring…",
    undoHeading: "Undoing the restore…",
    keepOpen: "Keep this page open. It updates by itself.",
    stepDone: "done",
    stepCurrent: "in progress",
    stepPending: "waiting",
    steps: {
      safety_backup: "Saving a copy of today's data",
      restore_scratch: "Reading the backup",
      migrate_scratch: "Bringing the backup up to this version",
      validate: "Checking the restored data",
      maintenance_on: "Pausing kiosks and staff phones",
      copy_forward: "Keeping backup settings and the audit log",
      swap: "Switching to the restored data",
      maintenance_off: "Resuming kiosks and staff phones",
      record: "Recording the restore",
    },
  },
  done: {
    heading: "Restored",
    body: "Restored from {date}. Sign in with the accounts as they were on that date.",
    undoneHeading: "Restore undone",
    undoneBody: "HDMS is back to how it was before the restore.",
    openAdmin: "Open HDMS admin",
    warnings: {
      maintenance_off_failed: "Kiosks may still show \"Under maintenance\". Ask IT to check the worker log.",
      record_failed: "The restore is done, but it could not be written to the audit log.",
    },
  },
  failed: {
    heading: "The restore did not finish",
    undoHeading: "The undo did not finish",
    body: "HDMS was left as it was before. Nothing was replaced.",
    again: "Pick a backup again",
    errors: {
      interrupted: "The server restarted during the restore.",
      no_admins: "This backup has no administrator account, so no one could sign in. Pick an older backup.",
      step: "It stopped at: {step}.",
    },
  },
};

export type RecoveryCatalogue = typeof en;
```

`hdms-frontend/apps/recovery/src/i18n/ja.ts`:

```ts
import type { RecoveryCatalogue } from "./en";

export const ja: RecoveryCatalogue = {
  app: {
    title: "HDMS 復旧",
    subtitle: "バックアップから HDMS を復旧します",
    language: "English",
    languageAria: "言語を切り替える",
  },
  common: {
    back: "戻る",
    retry: "再試行",
    loading: "読み込み中…",
    unreachable: "復旧サービスが応答しません。サーバーの電源を確認してから再試行してください。",
    unexpected: "問題が発生しました。再試行しても続く場合は、この画面を IT 担当者に見せてください。",
  },
  status: {
    heading: "HDMS の状態",
    working: "HDMS のデータベースは正常です",
    workingHint: "練習として復旧することもできます。復旧すると現在のデータは置き換えられます。",
    empty: "HDMS のデータベースは空です",
    emptyHint: "新しくインストールされたサーバーのようです。バックアップを復旧してデータを戻してください。",
    damaged: "HDMS のデータベースが破損しています",
    damagedHint: "HDMS がデータベースを使用できません。バックアップから復旧してください。",
    serverDown: "データベースサーバーが起動していません",
    serverDownHint: "IT 担当者に、災害復旧手順書の「データベースサーバーが起動しない」の項に沿った対応を依頼してください。その後、もう一度確認してください。",
    checkAgain: "もう一度確認",
    restoreRunning: "復旧を実行中です。進行状況を見るには復旧キーを入力してください。",
  },
  sources: {
    heading: "バックアップはどこにありますか？",
    local: "このサーバー",
    localHint: "このサーバーのディスクに保存されたバックアップ",
    folder: "ネットワークドライブまたは外付けディスク",
    noKey: "ここにはまだ復旧キーが保存されていません",
  },
  unlock: {
    heading: "復旧キーを入力",
    hint: "印刷した復旧シートに記載されています（7 組・28 文字の英数字）。",
    from: "バックアップ：{place}",
    label: "復旧キー",
    placeholder: "XXXX-XXXX-XXXX-XXXX-XXXX-XXXX-XXXX",
    submit: "解除",
    submitting: "確認中…",
    sessionEnded: "復旧セッションが終了しました。続けるには、もう一度キーを入力してください。",
    errors: {
      key_typo: "キーに入力ミスがあります。各組をシートと照らし合わせてください。",
      key_format: "復旧キーは 7 組・28 文字の英数字です。",
      key_wrong: "このキーでは、この場所のバックアップを開けません。",
      too_many_attempts: "試行回数が多すぎます。{minutes} 分待ってから再試行してください。",
      no_bundle: "このバックアップには復旧キーが保存されていません。",
      bundle_damaged: "この場所の復旧ファイルが破損しています。別の場所を試してください。",
      source_not_found: "この場所は利用できなくなりました。戻って選び直してください。",
    },
  },
  mismatch: {
    heading: "このサーバーは別のキーで設定されています",
    body: "バックアップは開けましたが、別のキーを持つサーバーのものです。ここで復旧すると、誰もサインインできないデータが戻ってしまいます。",
    action: "IT 担当者にこの復旧キーで install.sh --restore を実行してもらい、その後このページをもう一度開いてください。",
  },
  snapshots: {
    heading: "バックアップを選択",
    hint: "最新のバックアップが強調表示されています。",
    newest: "最新",
    when: "{date}（{ago}）",
    size: "サイズ {size}",
    choose: "このバックアップから復旧",
    empty: "この場所にはバックアップがありません。",
    unreadable: "この場所のバックアップを読み取れません。ドライブが接続されていることを確認してから再試行してください。",
    lastRestore: "{date} のバックアップから復旧しました。誤りだった場合は元に戻せます。",
    undo: "この復旧を元に戻す",
  },
  confirm: {
    heading: "復旧の確認",
    replace: "{date} 以降に記録された内容はすべて置き換えられます。",
    kiosks: "数分間キオスクが停止します。その間は紙の台帳をご利用ください。",
    undoHeading: "元に戻す操作の確認",
    undoBody: "HDMS を {date} のバックアップから復旧する直前の状態に戻します。",
    typeLabel: "確認のため RESTORE と入力してください",
    start: "復旧する",
    startUndo: "復旧を元に戻す",
    starting: "開始中…",
    errors: {
      database_server_down: "データベースサーバーが起動していません。IT 担当者に確認してから再試行してください。",
      snapshot_not_found: "このバックアップは見つかりません。戻って別のバックアップを選んでください。",
      nothing_to_undo: "元に戻す復旧はありません。",
    },
  },
  progress: {
    heading: "復旧中…",
    undoHeading: "復旧を元に戻しています…",
    keepOpen: "このページを開いたままにしてください。自動的に更新されます。",
    stepDone: "完了",
    stepCurrent: "実行中",
    stepPending: "待機中",
    steps: {
      safety_backup: "現在のデータのコピーを保存",
      restore_scratch: "バックアップを読み込み",
      migrate_scratch: "バックアップをこのバージョンに更新",
      validate: "復旧したデータを確認",
      maintenance_on: "キオスクと職員用アプリを一時停止",
      copy_forward: "バックアップ設定と監査ログを引き継ぎ",
      swap: "復旧したデータに切り替え",
      maintenance_off: "キオスクと職員用アプリを再開",
      record: "復旧を記録",
    },
  },
  done: {
    heading: "復旧しました",
    body: "{date} のバックアップから復旧しました。その時点のアカウントでサインインしてください。",
    undoneHeading: "復旧を元に戻しました",
    undoneBody: "HDMS は復旧前の状態に戻りました。",
    openAdmin: "HDMS 管理画面を開く",
    warnings: {
      maintenance_off_failed: "キオスクに「メンテナンス中」が表示されたままの可能性があります。IT 担当者にワーカーのログを確認してもらってください。",
      record_failed: "復旧は完了しましたが、監査ログに記録できませんでした。",
    },
  },
  failed: {
    heading: "復旧は完了しませんでした",
    undoHeading: "元に戻す操作は完了しませんでした",
    body: "HDMS は元の状態のままです。何も置き換えられていません。",
    again: "バックアップを選び直す",
    errors: {
      interrupted: "復旧中にサーバーが再起動しました。",
      no_admins: "このバックアップには管理者アカウントがないため、誰もサインインできません。より古いバックアップを選んでください。",
      step: "停止した手順：{step}",
    },
  },
};
```

`hdms-frontend/apps/recovery/src/i18n/index.ts`:

```ts
import { useTranslator, type Catalogues, type LeafKey } from "@hdms/i18n";
import { en, type RecoveryCatalogue } from "./en";
import { ja } from "./ja";

export const catalogues: Catalogues<RecoveryCatalogue> = { ja, en };

export type RecoveryKey = LeafKey<RecoveryCatalogue>;

export function useT() {
  return useTranslator(catalogues);
}

export type Translate = ReturnType<typeof useT>;

export { en, ja };
export type { RecoveryCatalogue };
```

`hdms-frontend/apps/recovery/src/i18n/no-literals.test.ts`: copy `hdms-frontend/apps/kiosk/src/i18n/no-literals.test.ts` and change the `describe` title to `"no user-visible string literals remain in recovery components"`.

- [ ] **Step 3: Write the failing library tests**

`hdms-frontend/apps/recovery/src/test/worker.ts`:

```ts
import { vi } from "vitest";

export type Reply = { status?: number; body?: unknown; headers?: Record<string, string> };

/**
 * A fake /recovery/api. Each "METHOD /path" (path without the /recovery/api
 * prefix) maps to one reply, or to a queue of replies that is consumed in
 * order with the last one repeating.
 */
export function fakeWorker(routes: Record<string, Reply | Reply[]>) {
  const calls: { route: string; body: unknown }[] = [];
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const url = new URL(String(input), "https://hdms.test");
    const route = `${init?.method ?? "GET"} ${url.pathname.replace(/^\/recovery\/api/, "")}`;
    const body = init?.body ? JSON.parse(String(init.body)) : undefined;
    calls.push({ route, body });
    const entry = routes[route];
    if (!entry) return new Response(JSON.stringify({ error: "not_found" }), { status: 404 });
    const reply = Array.isArray(entry) ? (entry.length > 1 ? entry.shift()! : entry[0]) : entry;
    return new Response(JSON.stringify(reply.body ?? {}), {
      status: reply.status ?? 200,
      headers: { "Content-Type": "application/json", ...reply.headers },
    });
  });
  return { calls, called: (route: string) => calls.filter((c) => c.route === route) };
}
```

`hdms-frontend/apps/recovery/src/test/fixtures.ts`:

```ts
import { STEPS, type RestoreView, type Snapshot, type Source, type Status } from "@/lib/api";

export const localSource: Source = { id: "local", kind: "local", folder: "/var/backups/hdms", hasRecoveryKey: true };
export const nasSource: Source = {
  id: "path:/mnt/nas/hdms-backups",
  kind: "destination",
  name: "Ward NAS",
  folder: "/mnt/nas/hdms-backups",
  hasRecoveryKey: true,
};
export const usbSource: Source = { id: "path:/mnt/nas/usb/hdms", kind: "folder", folder: "/mnt/nas/usb/hdms", hasRecoveryKey: true };
export const localWithoutKey: Source = { ...localSource, hasRecoveryKey: false };

export const workingStatus: Status = { database: "working", worker: "ready", restoreRunning: false };

// 2026-09-29T17:00Z is Wednesday 30 Sep, 02:00 in Tokyo.
export const snapshots: Snapshot[] = [
  { id: "snap-new", takenAt: "2026-09-29T17:00:00Z", sizeBytes: 1_500_000 },
  { id: "snap-old", takenAt: "2026-09-28T17:00:00Z", sizeBytes: 1_400_000 },
];

export function view(overrides: Partial<RestoreView> = {}): RestoreView {
  return {
    kind: "restore",
    phase: "running",
    step: "restore_scratch",
    steps: STEPS,
    sourceKind: "local",
    snapshotTakenAt: "2026-09-29T17:00:00Z",
    startedAt: "2026-10-01T00:00:00Z",
    canUndo: false,
    ...overrides,
  };
}
```

`hdms-frontend/apps/recovery/src/lib/api.test.ts`:

```ts
import { describe, expect, it, vi } from "vitest";
import { RecoveryError, asRecoveryError, recoveryApi } from "./api";
import { fakeWorker } from "@/test/worker";

describe("recoveryApi", () => {
  it("calls the worker under /recovery/api with the session cookie and JSON", async () => {
    const worker = fakeWorker({ "POST /unlock": { body: { keysMatch: true } } });
    await recoveryApi.unlock("local", "ABCD");
    expect(worker.called("POST /unlock")[0].body).toEqual({ source: "local", key: "ABCD" });
    const init = vi.mocked(globalThis.fetch).mock.calls[0][1];
    expect(vi.mocked(globalThis.fetch).mock.calls[0][0]).toBe("/recovery/api/unlock");
    expect(init?.credentials).toBe("same-origin");
  });

  it("turns {error} answers into a RecoveryError with the code and Retry-After", async () => {
    fakeWorker({ "POST /unlock": { status: 429, body: { error: "too_many_attempts" }, headers: { "Retry-After": "120" } } });
    const err = await recoveryApi.unlock("local", "ABCD").catch((e) => e);
    expect(err).toBeInstanceOf(RecoveryError);
    expect(err).toMatchObject({ status: 429, code: "too_many_attempts", retryAfterSeconds: 120 });
  });

  it("knows a lost session when it sees one", async () => {
    fakeWorker({ "GET /snapshots": { status: 401, body: { error: "session_required" } } });
    const err = await recoveryApi.snapshots().catch(asRecoveryError);
    expect(err.sessionLost).toBe(true);
  });

  it("reports an unreachable worker as status 0", async () => {
    vi.spyOn(globalThis, "fetch").mockRejectedValue(new TypeError("Failed to fetch"));
    const err = await recoveryApi.status().catch(asRecoveryError);
    expect(err).toMatchObject({ status: 0, code: "unreachable" });
  });

  it("unwraps list and view payloads", async () => {
    fakeWorker({
      "GET /sources": { body: { sources: [{ id: "local" }] } },
      "GET /restore": { body: { restore: null } },
    });
    expect(await recoveryApi.sources()).toEqual([{ id: "local" }]);
    expect(await recoveryApi.restore()).toBeNull();
  });
});
```

`hdms-frontend/apps/recovery/src/lib/format.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { formatAgo, formatBytes, formatSnapshotDate } from "./format";

describe("format", () => {
  it("formatBytes uses decimal units like a disk label", () => {
    expect(formatBytes(0)).toBe("0 B");
    expect(formatBytes(999)).toBe("999 B");
    expect(formatBytes(1_500_000)).toBe("1.5 MB");
    expect(formatBytes(2_000_000_000)).toBe("2.0 GB");
  });

  it("formatSnapshotDate shows hospital time with the weekday", () => {
    const en = formatSnapshotDate("en", "2026-09-29T17:00:00Z");
    expect(en).toContain("Wednesday");
    expect(en).toContain("Sep 30");
    expect(en).toContain("02:00");
    expect(formatSnapshotDate("ja", "2026-09-29T17:00:00Z")).toContain("水曜日");
  });

  it("formatAgo counts back in minutes, hours, then days", () => {
    const now = new Date("2026-09-30T17:00:00Z");
    expect(formatAgo("en", "2026-09-30T16:59:50Z", now)).toBe("1 minute ago");
    expect(formatAgo("en", "2026-09-30T14:00:00Z", now)).toBe("3 hours ago");
    expect(formatAgo("en", "2026-09-29T17:00:00Z", now)).toBe("1 day ago");
    expect(formatAgo("ja", "2026-09-29T17:00:00Z", now)).toMatch(/1\s?日前/);
  });
});
```

Run: `cd hdms-frontend && pnpm --filter recovery test`
Expected: FAIL — `Failed to resolve import "./api"` / `"./format"`.

- [ ] **Step 4: Write the library**

`hdms-frontend/apps/recovery/src/lib/api.ts`:

```ts
// The worker's recovery API (/recovery/api/*). It is not in the OpenAPI
// contract — the API process is not involved — so the shapes live here and
// must match hdms-backend/internal/platform/recovery/handler.go and state.go.

export type LiveState = "working" | "empty" | "damaged" | "server_down";
export type WorkerMode = "starting" | "database_unavailable" | "ready";
export type SourceKind = "local" | "folder" | "destination";
export type Step =
  | "safety_backup"
  | "restore_scratch"
  | "migrate_scratch"
  | "validate"
  | "maintenance_on"
  | "copy_forward"
  | "swap"
  | "maintenance_off"
  | "record";

/** Every step a full restore runs, in order (engine.go `plan`). */
export const STEPS: Step[] = [
  "safety_backup",
  "restore_scratch",
  "migrate_scratch",
  "validate",
  "maintenance_on",
  "copy_forward",
  "swap",
  "maintenance_off",
  "record",
];

/** What the worker requires typed before a restore or an undo. */
export const CONFIRM_WORD = "RESTORE";

export interface Status {
  database: LiveState;
  worker: WorkerMode;
  restoreRunning: boolean;
}

export interface Source {
  id: string;
  kind: SourceKind;
  name?: string;
  folder: string;
  hasRecoveryKey: boolean;
}

export interface Snapshot {
  id: string;
  takenAt: string;
  sizeBytes: number;
}

export interface RestoreView {
  kind: "restore" | "undo";
  phase: "running" | "completed" | "failed";
  step: Step;
  steps: Step[];
  sourceKind: SourceKind;
  sourceName?: string;
  snapshotTakenAt: string;
  startedAt: string;
  finishedAt?: string;
  error?: string;
  warning?: string;
  canUndo: boolean;
}

export class RecoveryError extends Error {
  readonly status: number;
  readonly code: string;
  readonly retryAfterSeconds?: number;

  constructor(status: number, code: string, retryAfterSeconds?: number) {
    super(code);
    this.name = "RecoveryError";
    this.status = status;
    this.code = code;
    this.retryAfterSeconds = retryAfterSeconds;
  }

  /** The worker no longer knows this browser: 30 minutes idle, or it restarted. */
  get sessionLost(): boolean {
    return this.status === 401 && this.code === "session_required";
  }
}

export function asRecoveryError(err: unknown): RecoveryError {
  return err instanceof RecoveryError ? err : new RecoveryError(0, "unexpected");
}

const API_BASE = "/recovery/api";

async function call<T>(method: "GET" | "POST", path: string, body?: unknown): Promise<T> {
  let res: Response;
  try {
    res = await fetch(`${API_BASE}${path}`, {
      method,
      credentials: "same-origin",
      headers: body === undefined ? undefined : { "Content-Type": "application/json" },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  } catch {
    throw new RecoveryError(0, "unreachable");
  }
  const payload = (await res.json().catch(() => ({}))) as Record<string, unknown>;
  if (!res.ok) {
    const code = typeof payload.error === "string" ? payload.error : "unexpected";
    const retry = Number(res.headers.get("Retry-After"));
    throw new RecoveryError(res.status, code, Number.isFinite(retry) && retry > 0 ? retry : undefined);
  }
  return payload as T;
}

export const recoveryApi = {
  status: () => call<Status>("GET", "/status"),
  sources: () => call<{ sources: Source[] }>("GET", "/sources").then((r) => r.sources),
  unlock: (source: string, key: string) => call<{ keysMatch: true }>("POST", "/unlock", { source, key }),
  snapshots: () => call<{ snapshots: Snapshot[] }>("GET", "/snapshots").then((r) => r.snapshots),
  restore: () => call<{ restore: RestoreView | null }>("GET", "/restore").then((r) => r.restore),
  startRestore: (snapshotId: string) =>
    call<{ restore: RestoreView }>("POST", "/restore", { snapshotId, confirmation: CONFIRM_WORD }).then((r) => r.restore),
  undo: () => call<{ restore: RestoreView }>("POST", "/restore/undo", { confirmation: CONFIRM_WORD }).then((r) => r.restore),
};
```

`hdms-frontend/apps/recovery/src/lib/format.ts`:

```ts
import { TIMEZONE, type Locale } from "@hdms/i18n";

const UNITS = ["B", "KB", "MB", "GB", "TB"];

/** Decimal units: people compare this with what a disk's label says. */
export function formatBytes(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return "0 B";
  const i = Math.min(Math.floor(Math.log10(n) / 3), UNITS.length - 1);
  if (i === 0) return `${n} B`;
  return `${(n / 1000 ** i).toFixed(1)} ${UNITS[i]}`;
}

/** "Wednesday, Sep 30, 02:00" — hospital time, whatever the browser's zone. */
export function formatSnapshotDate(locale: Locale, iso: string): string {
  return new Intl.DateTimeFormat(locale, {
    weekday: "long",
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
    timeZone: TIMEZONE,
  }).format(new Date(iso));
}

/** "1 day ago". Snapshots are always in the past, so the floor is one minute. */
export function formatAgo(locale: Locale, iso: string, now: Date): string {
  const rtf = new Intl.RelativeTimeFormat(locale, { numeric: "always" });
  const minutes = Math.max(1, Math.round((now.getTime() - new Date(iso).getTime()) / 60_000));
  if (minutes < 60) return rtf.format(-minutes, "minute");
  const hours = Math.round(minutes / 60);
  if (hours < 24) return rtf.format(-hours, "hour");
  return rtf.format(-Math.round(hours / 24), "day");
}
```

`hdms-frontend/apps/recovery/src/lib/errors.ts`:

```ts
import type { Translate } from "@/i18n";
import type { RecoveryError } from "./api";

/** The message for an error no screen has a better word for. */
export function commonError(t: Translate, err: RecoveryError): string {
  return t(err.status === 0 ? "common.unreachable" : "common.unexpected");
}
```

`hdms-frontend/apps/recovery/src/lib/source.ts`:

```ts
import type { Translate } from "@/i18n";
import type { Source } from "./api";

export function sourceTitle(t: Translate, s: Source): string {
  if (s.kind === "local") return t("sources.local");
  if (s.kind === "destination" && s.name) return s.name;
  return t("sources.folder");
}

export function sourceDetail(t: Translate, s: Source): string {
  return s.kind === "local" ? t("sources.localHint") : s.folder;
}
```

Run: `cd hdms-frontend && pnpm --filter recovery test -- lib`
Expected: PASS (`api.test.ts`, `format.test.ts`).

- [ ] **Step 5: Write the failing screen tests**

`hdms-frontend/apps/recovery/src/screens/start-screen.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { axe } from "vitest-axe";
import { LocaleProvider } from "@hdms/i18n";
import { StartScreen } from "./start-screen";
import { fakeWorker } from "@/test/worker";
import { localSource, localWithoutKey, nasSource, usbSource, workingStatus } from "@/test/fixtures";

function renderStart() {
  const onChoose = vi.fn();
  const view = render(
    <LocaleProvider locale="en">
      <StartScreen onChoose={onChoose} />
    </LocaleProvider>,
  );
  return { onChoose, container: view.container };
}

describe("StartScreen", () => {
  it.each([
    ["working", "HDMS database is working"],
    ["empty", "HDMS database is empty"],
    ["damaged", "HDMS database is damaged"],
  ])("says what the worker sees when the database is %s", async (database, text) => {
    fakeWorker({
      "GET /status": { body: { ...workingStatus, database } },
      "GET /sources": { body: { sources: [localSource] } },
    });
    renderStart();
    expect(await screen.findByTestId("database-status")).toHaveTextContent(text);
  });

  it("lists every place with backups and refuses one without a recovery key", async () => {
    fakeWorker({
      "GET /status": { body: workingStatus },
      "GET /sources": { body: { sources: [localWithoutKey, nasSource, usbSource] } },
    });
    const user = userEvent.setup();
    const { onChoose, container } = renderStart();

    const local = await screen.findByTestId("source-local");
    expect(local).toHaveTextContent("This server");
    expect(local).toHaveTextContent("No recovery key is stored here yet");
    expect(local).toBeDisabled();
    expect(screen.getByTestId(`source-${nasSource.id}`)).toHaveTextContent("Ward NAS");
    expect(screen.getByTestId(`source-${usbSource.id}`)).toHaveTextContent("Network drive or external disk");
    expect(screen.getByTestId(`source-${usbSource.id}`)).toHaveTextContent("/mnt/nas/usb/hdms");

    await user.click(screen.getByTestId(`source-${usbSource.id}`));
    expect(onChoose).toHaveBeenCalledWith(usbSource);
    expect(await axe(container)).toHaveNoViolations();
  });

  it("offers no source while the database server is down, and checks again on request", async () => {
    const worker = fakeWorker({
      "GET /status": { body: { ...workingStatus, database: "server_down" } },
      "GET /sources": { body: { sources: [localSource] } },
    });
    const user = userEvent.setup();
    renderStart();

    expect(await screen.findByTestId("database-status")).toHaveTextContent("The database server is not running");
    expect(screen.queryByTestId("source-local")).toBeNull();
    expect(worker.called("GET /sources")).toHaveLength(0);

    await user.click(screen.getByRole("button", { name: "Check again" }));
    await screen.findByTestId("database-status");
    expect(worker.called("GET /status").length).toBeGreaterThanOrEqual(2);
  });

  it("says when a restore is already running", async () => {
    fakeWorker({
      "GET /status": { body: { ...workingStatus, restoreRunning: true } },
      "GET /sources": { body: { sources: [localSource] } },
    });
    renderStart();
    expect(await screen.findByTestId("restore-running")).toHaveTextContent("A restore is running");
  });

  it("says plainly when the worker does not answer", async () => {
    vi.spyOn(globalThis, "fetch").mockRejectedValue(new TypeError("Failed to fetch"));
    renderStart();
    expect(await screen.findByTestId("start-error")).toHaveTextContent("not responding");
    expect(screen.getByRole("button", { name: "Try again" })).toBeInTheDocument();
  });
});
```

`hdms-frontend/apps/recovery/src/screens/unlock-screen.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { axe } from "vitest-axe";
import { LocaleProvider } from "@hdms/i18n";
import { UnlockScreen } from "./unlock-screen";
import { fakeWorker } from "@/test/worker";
import { localSource, nasSource } from "@/test/fixtures";

const KEY = "ABCD-EFGH-JKMN-PQRS-TVWX-YZ01-2345";

function renderUnlock(props: Partial<Parameters<typeof UnlockScreen>[0]> = {}) {
  const handlers = { onUnlocked: vi.fn(), onMismatch: vi.fn(), onBack: vi.fn() };
  const view = render(
    <LocaleProvider locale="en">
      <UnlockScreen source={localSource} sessionEnded={false} {...handlers} {...props} />
    </LocaleProvider>,
  );
  return { ...handlers, container: view.container };
}

async function typeKeyAndSubmit(key = KEY) {
  const user = userEvent.setup();
  await user.type(screen.getByLabelText("Recovery key"), key);
  await user.click(screen.getByRole("button", { name: "Unlock" }));
  return user;
}

describe("UnlockScreen", () => {
  it("unlocks the chosen place with the typed key", async () => {
    const worker = fakeWorker({ "POST /unlock": { body: { keysMatch: true } } });
    const { onUnlocked, container } = renderUnlock({ source: nasSource });
    expect(screen.getByText("Backups: Ward NAS")).toBeInTheDocument();
    expect(await axe(container)).toHaveNoViolations();

    await typeKeyAndSubmit();

    expect(onUnlocked).toHaveBeenCalledTimes(1);
    expect(worker.called("POST /unlock")[0].body).toEqual({ source: nasSource.id, key: KEY });
  });

  it("keeps Unlock disabled until something is typed", () => {
    fakeWorker({});
    renderUnlock();
    expect(screen.getByRole("button", { name: "Unlock" })).toBeDisabled();
  });

  it.each([
    [422, "key_typo", "typing mistake"],
    [422, "key_format", "28 letters and numbers in 7 groups"],
    [401, "key_wrong", "does not open the backups"],
    [404, "no_bundle", "No recovery key is stored"],
    [422, "bundle_damaged", "damaged"],
    [404, "source_not_found", "no longer available"],
    [500, "internal", "Something went wrong"],
  ])("explains %i %s in plain words and keeps the key for correction", async (status, code, text) => {
    fakeWorker({ "POST /unlock": { status, body: { error: code } } });
    const { onUnlocked } = renderUnlock();
    await typeKeyAndSubmit();

    expect(await screen.findByTestId("unlock-error")).toHaveTextContent(text);
    expect(screen.getByLabelText("Recovery key")).toHaveValue(KEY);
    expect(onUnlocked).not.toHaveBeenCalled();
  });

  it("says how long to wait after too many attempts", async () => {
    fakeWorker({ "POST /unlock": { status: 429, body: { error: "too_many_attempts" }, headers: { "Retry-After": "90" } } });
    renderUnlock();
    await typeKeyAndSubmit();
    expect(await screen.findByTestId("unlock-error")).toHaveTextContent("Wait 2 min");
  });

  it("hands a keys mismatch to the mismatch screen and forgets the key", async () => {
    fakeWorker({ "POST /unlock": { status: 409, body: { error: "keys_mismatch" } } });
    const { onMismatch } = renderUnlock();
    await typeKeyAndSubmit();
    expect(onMismatch).toHaveBeenCalledTimes(1);
    expect(screen.getByLabelText("Recovery key")).toHaveValue("");
  });

  it("explains why the key is asked for again after a lost session", () => {
    fakeWorker({});
    renderUnlock({ sessionEnded: true });
    expect(screen.getByText(/recovery session ended/)).toBeInTheDocument();
  });
});
```

`hdms-frontend/apps/recovery/src/screens/mismatch-screen.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@hdms/i18n";
import { MismatchScreen } from "./mismatch-screen";

describe("MismatchScreen", () => {
  it("blocks the restore and says what IT must run", async () => {
    const onBack = vi.fn();
    render(
      <LocaleProvider locale="en">
        <MismatchScreen onBack={onBack} />
      </LocaleProvider>,
    );
    expect(screen.getByRole("heading", { name: "This server was set up with different keys" })).toBeInTheDocument();
    expect(screen.getByText(/install\.sh --restore/)).toBeInTheDocument();
    await userEvent.setup().click(screen.getByRole("button", { name: "Back" }));
    expect(onBack).toHaveBeenCalledTimes(1);
  });
});
```

Run: `cd hdms-frontend && pnpm --filter recovery test`
Expected: FAIL — `Failed to resolve import "./start-screen"` (and `./unlock-screen`, `./mismatch-screen`).

- [ ] **Step 6: Write the components and screens**

`hdms-frontend/apps/recovery/src/components/ui/button-variants.ts`:

```ts
import { cva } from "class-variance-authority";

export const buttonVariants = cva(
  "inline-flex shrink-0 items-center justify-center gap-2 rounded-md text-sm font-medium whitespace-nowrap transition-all outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50 disabled:pointer-events-none disabled:opacity-50 [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
  {
    variants: {
      variant: {
        default: "bg-primary text-primary-foreground hover:bg-primary/90",
        destructive: "bg-destructive text-white hover:bg-destructive/90",
        outline: "border bg-background shadow-xs hover:bg-accent hover:text-accent-foreground",
      },
      size: {
        default: "h-10 px-4 py-2",
        lg: "h-12 px-6 text-base",
      },
    },
    defaultVariants: { variant: "default", size: "default" },
  },
);
```

`hdms-frontend/apps/recovery/src/components/ui/button.tsx`:

```tsx
import * as React from "react";
import type { VariantProps } from "class-variance-authority";
import { cn } from "@hdms/ui";
import { buttonVariants } from "./button-variants";

export function Button({
  className,
  variant,
  size,
  type = "button",
  ...props
}: React.ComponentProps<"button"> & VariantProps<typeof buttonVariants>) {
  return <button type={type} className={cn(buttonVariants({ variant, size, className }))} {...props} />;
}
```

`hdms-frontend/apps/recovery/src/components/message.tsx`:

```tsx
import * as React from "react";
import { cn } from "@hdms/ui";

const TONES = {
  info: "border-primary/30 bg-primary/5 text-foreground",
  warning: "border-warning/50 bg-warning/10 text-foreground",
  error: "border-destructive/40 bg-destructive/5 text-destructive",
} as const;

export function Message({
  tone,
  id,
  testId,
  children,
}: {
  tone: keyof typeof TONES;
  id?: string;
  testId?: string;
  children: React.ReactNode;
}) {
  return (
    <div
      id={id}
      data-testid={testId}
      role={tone === "error" ? "alert" : "status"}
      className={cn("rounded-lg border p-4 text-sm", TONES[tone])}
    >
      {children}
    </div>
  );
}
```

`hdms-frontend/apps/recovery/src/components/page.tsx`:

```tsx
import * as React from "react";
import { useLocale } from "@hdms/i18n";
import { Button } from "@/components/ui/button";
import { useT } from "@/i18n";

export function Page({ children }: { children: React.ReactNode }) {
  const t = useT();
  const { locale, setLocale } = useLocale();
  return (
    <div className="min-h-dvh bg-background text-foreground">
      <header className="border-b">
        <div className="mx-auto flex max-w-2xl items-center justify-between gap-4 px-4 py-4">
          <div>
            <h1 className="text-xl font-bold">{t("app.title")}</h1>
            <p className="text-sm text-muted-foreground">{t("app.subtitle")}</p>
          </div>
          <Button
            variant="outline"
            data-testid="language-toggle"
            aria-label={t("app.languageAria")}
            onClick={() => setLocale(locale === "ja" ? "en" : "ja")}
          >
            {t("app.language")}
          </Button>
        </div>
      </header>
      <main className="mx-auto max-w-2xl px-4 py-8">{children}</main>
    </div>
  );
}
```

`hdms-frontend/apps/recovery/src/screens/start-screen.tsx`:

```tsx
import * as React from "react";
import { Message } from "@/components/message";
import { Button } from "@/components/ui/button";
import { useT, type RecoveryKey } from "@/i18n";
import { asRecoveryError, recoveryApi, type LiveState, type RecoveryError, type Source, type Status } from "@/lib/api";
import { commonError } from "@/lib/errors";
import { sourceDetail, sourceTitle } from "@/lib/source";

const DATABASE_TEXT: Record<LiveState, { title: RecoveryKey; hint: RecoveryKey; tone: "info" | "error" }> = {
  working: { title: "status.working", hint: "status.workingHint", tone: "info" },
  empty: { title: "status.empty", hint: "status.emptyHint", tone: "info" },
  damaged: { title: "status.damaged", hint: "status.damagedHint", tone: "error" },
  server_down: { title: "status.serverDown", hint: "status.serverDownHint", tone: "error" },
};

type Loaded = { status: Status; sources: Source[] };

export function StartScreen({ onChoose }: { onChoose: (source: Source) => void }) {
  const t = useT();
  const [loaded, setLoaded] = React.useState<Loaded | null>(null);
  const [error, setError] = React.useState<RecoveryError | null>(null);

  const load = React.useCallback(async () => {
    setLoaded(null);
    setError(null);
    try {
      const status = await recoveryApi.status();
      // Nothing can be restored into a database server that is not running.
      const sources = status.database === "server_down" ? [] : await recoveryApi.sources();
      setLoaded({ status, sources });
    } catch (err) {
      setError(asRecoveryError(err));
    }
  }, []);

  React.useEffect(() => {
    void load();
  }, [load]);

  if (error) {
    return (
      <section className="space-y-4">
        <Message tone="error" testId="start-error">
          {commonError(t, error)}
        </Message>
        <Button onClick={() => void load()}>{t("common.retry")}</Button>
      </section>
    );
  }
  if (!loaded) return <p className="text-muted-foreground">{t("common.loading")}</p>;

  const { status, sources } = loaded;
  const text = DATABASE_TEXT[status.database] ?? DATABASE_TEXT.damaged;
  return (
    <section className="space-y-8">
      <div className="space-y-3">
        <h2 className="text-lg font-semibold">{t("status.heading")}</h2>
        <Message tone={text.tone} testId="database-status">
          <p className="font-semibold">{t(text.title)}</p>
          <p className="mt-1">{t(text.hint)}</p>
        </Message>
        {status.restoreRunning && (
          <Message tone="info" testId="restore-running">
            {t("status.restoreRunning")}
          </Message>
        )}
      </div>

      {status.database === "server_down" ? (
        <Button onClick={() => void load()}>{t("status.checkAgain")}</Button>
      ) : (
        <div className="space-y-3">
          <h2 className="text-lg font-semibold">{t("sources.heading")}</h2>
          <ul className="space-y-3">
            {sources.map((s) => (
              <li key={s.id}>
                <button
                  type="button"
                  data-testid={`source-${s.id}`}
                  disabled={!s.hasRecoveryKey}
                  onClick={() => onChoose(s)}
                  className="w-full rounded-lg border bg-background p-4 text-left transition-colors hover:bg-accent focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:opacity-60"
                >
                  <span className="block font-semibold">{sourceTitle(t, s)}</span>
                  <span className="block break-all text-sm text-muted-foreground">{sourceDetail(t, s)}</span>
                  {!s.hasRecoveryKey && (
                    <span className="mt-1 block text-sm text-destructive">{t("sources.noKey")}</span>
                  )}
                </button>
              </li>
            ))}
          </ul>
        </div>
      )}
    </section>
  );
}
```

`hdms-frontend/apps/recovery/src/screens/unlock-screen.tsx`:

```tsx
import * as React from "react";
import { Message } from "@/components/message";
import { Button } from "@/components/ui/button";
import { useT, type RecoveryKey, type Translate } from "@/i18n";
import { asRecoveryError, recoveryApi, type RecoveryError, type Source } from "@/lib/api";
import { commonError } from "@/lib/errors";
import { sourceTitle } from "@/lib/source";

const UNLOCK_ERRORS: Record<string, RecoveryKey> = {
  key_typo: "unlock.errors.key_typo",
  key_format: "unlock.errors.key_format",
  key_wrong: "unlock.errors.key_wrong",
  no_bundle: "unlock.errors.no_bundle",
  bundle_damaged: "unlock.errors.bundle_damaged",
  source_not_found: "unlock.errors.source_not_found",
};

function unlockError(t: Translate, err: RecoveryError): string {
  if (err.code === "too_many_attempts") {
    const minutes = Math.max(1, Math.ceil((err.retryAfterSeconds ?? 60) / 60));
    return t("unlock.errors.too_many_attempts", { minutes });
  }
  const key = UNLOCK_ERRORS[err.code];
  return key ? t(key) : commonError(t, err);
}

export function UnlockScreen({
  source,
  sessionEnded,
  onUnlocked,
  onMismatch,
  onBack,
}: {
  source: Source;
  sessionEnded: boolean;
  onUnlocked: () => void;
  onMismatch: () => void;
  onBack: () => void;
}) {
  const t = useT();
  const [key, setKey] = React.useState("");
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState<RecoveryError | null>(null);

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    if (busy || key.trim() === "") return;
    setBusy(true);
    setError(null);
    try {
      await recoveryApi.unlock(source.id, key);
      setKey("");
      onUnlocked();
    } catch (err) {
      const e = asRecoveryError(err);
      if (e.code === "keys_mismatch") {
        setKey("");
        onMismatch();
        return;
      }
      // The key stays in the field: retyping 28 characters is how a second
      // mistake gets made.
      setError(e);
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="space-y-6">
      <div className="space-y-2">
        <h2 className="text-lg font-semibold">{t("unlock.heading")}</h2>
        <p className="text-sm text-muted-foreground">{t("unlock.from", { place: sourceTitle(t, source) })}</p>
        <p className="text-sm">{t("unlock.hint")}</p>
      </div>
      {sessionEnded && <Message tone="info">{t("unlock.sessionEnded")}</Message>}
      <form onSubmit={submit} className="space-y-4" noValidate>
        <div className="space-y-2">
          <label htmlFor="recovery-key" className="block text-sm font-medium">
            {t("unlock.label")}
          </label>
          <input
            id="recovery-key"
            name="recovery-key"
            value={key}
            onChange={(e) => setKey(e.target.value)}
            autoComplete="off"
            autoCapitalize="characters"
            spellCheck={false}
            placeholder={t("unlock.placeholder")}
            aria-invalid={error ? true : undefined}
            aria-describedby={error ? "unlock-error" : undefined}
            className="h-12 w-full rounded-md border bg-background px-3 font-mono text-lg uppercase tracking-wider focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
          />
        </div>
        {error && (
          <Message tone="error" id="unlock-error" testId="unlock-error">
            {unlockError(t, error)}
          </Message>
        )}
        <div className="flex flex-wrap gap-3">
          <Button type="submit" size="lg" disabled={busy || key.trim() === ""}>
            {busy ? t("unlock.submitting") : t("unlock.submit")}
          </Button>
          <Button variant="outline" size="lg" onClick={onBack}>
            {t("common.back")}
          </Button>
        </div>
      </form>
    </section>
  );
}
```

`hdms-frontend/apps/recovery/src/screens/mismatch-screen.tsx`:

```tsx
import { Message } from "@/components/message";
import { Button } from "@/components/ui/button";
import { useT } from "@/i18n";

// The bundle opened, but its secrets are not this server's: restoring would
// bring back badges and TOTP no one could use. Only install.sh --restore
// (plan 4) can give this server the right keys.
export function MismatchScreen({ onBack }: { onBack: () => void }) {
  const t = useT();
  return (
    <section className="space-y-6">
      <h2 className="text-lg font-semibold">{t("mismatch.heading")}</h2>
      <Message tone="error" testId="keys-mismatch">
        <p>{t("mismatch.body")}</p>
        <p className="mt-2 font-semibold">{t("mismatch.action")}</p>
      </Message>
      <Button variant="outline" onClick={onBack}>
        {t("common.back")}
      </Button>
    </section>
  );
}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `cd hdms-frontend && pnpm --filter recovery test && pnpm --filter recovery lint && pnpm --filter recovery exec tsc -b`
Expected: PASS (`no-literals` included), lint clean, `tsc -b` exits 0. (`vite build` waits for `src/main.tsx` in Task 7.)

- [ ] **Step 8: Commit**

```bash
git add hdms-frontend/apps/recovery hdms-frontend/pnpm-lock.yaml
git commit -m "feat(recovery): recovery app with status, backup places and key entry"
```

---

### Task 6: Recovery app — pick a backup, confirm, progress

**Files:**
- Create: `hdms-frontend/apps/recovery/src/screens/{snapshots-screen.tsx,confirm-screen.tsx,progress-screen.tsx}`
- Test: `hdms-frontend/apps/recovery/src/screens/{snapshots-screen.test.tsx,confirm-screen.test.tsx,progress-screen.test.tsx}`

**Interfaces:**
- Consumes: everything Task 5 produced; the worker's `GET /restore` → `{restore: RestoreView | null}`, `GET /snapshots` → `{snapshots}` newest first, `POST /restore {snapshotId, confirmation}` → `202`, `POST /restore/undo {confirmation}` → `202`, errors `restore_running`, `nothing_to_undo`, `database_server_down`, `snapshot_not_found`, `confirmation_required`, `repository_unreadable` (502).
- Produces:
  - `SnapshotsScreen({ onPick(snapshot), onUndo(snapshotTakenAt), onRunning(), onSessionLost() })`
  - `type ConfirmMode = { kind: "restore"; snapshot: Snapshot } | { kind: "undo"; snapshotTakenAt: string }`; `ConfirmScreen({ mode, onStarted(), onBack(), onSessionLost() })`
  - `POLL_MS = 2000`; `ProgressScreen({ pollMs?, onUndo(snapshotTakenAt), onRestart(), onSessionLost() })`; `ADMIN_URL = "/admin/"`

- [ ] **Step 1: Write the failing tests**

`hdms-frontend/apps/recovery/src/screens/snapshots-screen.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@hdms/i18n";
import { SnapshotsScreen } from "./snapshots-screen";
import { fakeWorker } from "@/test/worker";
import { snapshots, view } from "@/test/fixtures";

function renderSnapshots() {
  const handlers = { onPick: vi.fn(), onUndo: vi.fn(), onRunning: vi.fn(), onSessionLost: vi.fn() };
  render(
    <LocaleProvider locale="en">
      <SnapshotsScreen {...handlers} />
    </LocaleProvider>,
  );
  return handlers;
}

describe("SnapshotsScreen", () => {
  it("lists backups newest first, highlights the newest and shows plain dates and sizes", async () => {
    fakeWorker({ "GET /restore": { body: { restore: null } }, "GET /snapshots": { body: { snapshots } } });
    const { onPick } = renderSnapshots();

    const rows = await screen.findAllByTestId("snapshot");
    expect(rows).toHaveLength(2);
    expect(rows[0]).toHaveAttribute("data-newest", "true");
    expect(rows[0]).toHaveTextContent("Newest");
    expect(rows[0]).toHaveTextContent("Sep 30");
    expect(rows[0]).toHaveTextContent("02:00");
    expect(rows[0]).toHaveTextContent("Size 1.5 MB");
    expect(rows[1]).not.toHaveAttribute("data-newest");

    await userEvent.setup().click(screen.getAllByRole("button", { name: "Restore this backup" })[0]);
    expect(onPick).toHaveBeenCalledWith(snapshots[0]);
  });

  it("goes straight to progress when a restore is already running", async () => {
    const worker = fakeWorker({ "GET /restore": { body: { restore: view() } }, "GET /snapshots": { body: { snapshots } } });
    const { onRunning } = renderSnapshots();
    await vi.waitFor(() => expect(onRunning).toHaveBeenCalledTimes(1));
    expect(worker.called("GET /snapshots")).toHaveLength(0);
  });

  it("offers to undo the last restore while the previous database is kept", async () => {
    fakeWorker({
      "GET /restore": { body: { restore: view({ phase: "completed", step: "record", canUndo: true }) } },
      "GET /snapshots": { body: { snapshots } },
    });
    const { onUndo } = renderSnapshots();
    expect(await screen.findByTestId("last-restore")).toHaveTextContent("you can undo it");
    await userEvent.setup().click(screen.getByRole("button", { name: "Undo this restore" }));
    expect(onUndo).toHaveBeenCalledWith("2026-09-29T17:00:00Z");
  });

  it("hands a lost session back to key entry", async () => {
    fakeWorker({ "GET /restore": { status: 401, body: { error: "session_required" } } });
    const { onSessionLost } = renderSnapshots();
    await vi.waitFor(() => expect(onSessionLost).toHaveBeenCalledTimes(1));
  });

  it("says when the backups cannot be read, and when there are none", async () => {
    fakeWorker({
      "GET /restore": { body: { restore: null } },
      "GET /snapshots": [{ status: 502, body: { error: "repository_unreadable" } }, { body: { snapshots: [] } }],
    });
    renderSnapshots();
    expect(await screen.findByTestId("snapshots-error")).toHaveTextContent("cannot be read");
    await userEvent.setup().click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByText("There are no backups in this place.")).toBeInTheDocument();
  });
});
```

`hdms-frontend/apps/recovery/src/screens/confirm-screen.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@hdms/i18n";
import { ConfirmScreen, type ConfirmMode } from "./confirm-screen";
import { fakeWorker } from "@/test/worker";
import { snapshots, view } from "@/test/fixtures";

function renderConfirm(mode: ConfirmMode) {
  const handlers = { onStarted: vi.fn(), onBack: vi.fn(), onSessionLost: vi.fn() };
  render(
    <LocaleProvider locale="en">
      <ConfirmScreen mode={mode} {...handlers} />
    </LocaleProvider>,
  );
  return handlers;
}

describe("ConfirmScreen", () => {
  it("says what will be replaced and starts only once RESTORE is typed", async () => {
    const worker = fakeWorker({ "POST /restore": { status: 202, body: { restore: view() } } });
    const { onStarted } = renderConfirm({ kind: "restore", snapshot: snapshots[0] });
    const user = userEvent.setup();

    expect(screen.getByText(/Everything recorded after .*Sep 30.* will be replaced/)).toBeInTheDocument();
    expect(screen.getByText(/paper register/)).toBeInTheDocument();

    const start = screen.getByRole("button", { name: "Restore" });
    await user.type(screen.getByLabelText("Type RESTORE to confirm"), "restor");
    expect(start).toBeDisabled();
    await user.type(screen.getByLabelText("Type RESTORE to confirm"), "e");
    expect(start).toBeEnabled();
    await user.click(start);

    expect(onStarted).toHaveBeenCalledTimes(1);
    expect(worker.called("POST /restore")[0].body).toEqual({ snapshotId: "snap-new", confirmation: "RESTORE" });
  });

  it("undoes with the same confirmation", async () => {
    const worker = fakeWorker({ "POST /restore/undo": { status: 202, body: { restore: view({ kind: "undo" }) } } });
    const { onStarted } = renderConfirm({ kind: "undo", snapshotTakenAt: "2026-09-29T17:00:00Z" });
    const user = userEvent.setup();
    expect(screen.getByText(/just before the restore from/)).toBeInTheDocument();
    await user.type(screen.getByLabelText("Type RESTORE to confirm"), "RESTORE");
    await user.click(screen.getByRole("button", { name: "Undo the restore" }));
    expect(onStarted).toHaveBeenCalledTimes(1);
    expect(worker.called("POST /restore/undo")[0].body).toEqual({ confirmation: "RESTORE" });
  });

  it("treats a restore that is already running as started", async () => {
    fakeWorker({ "POST /restore": { status: 409, body: { error: "restore_running" } } });
    const { onStarted } = renderConfirm({ kind: "restore", snapshot: snapshots[0] });
    const user = userEvent.setup();
    await user.type(screen.getByLabelText("Type RESTORE to confirm"), "RESTORE");
    await user.click(screen.getByRole("button", { name: "Restore" }));
    expect(onStarted).toHaveBeenCalledTimes(1);
  });

  it.each([
    ["database_server_down", "database server is not running"],
    ["snapshot_not_found", "no longer there"],
  ])("explains %s", async (code, text) => {
    fakeWorker({ "POST /restore": { status: 409, body: { error: code } } });
    renderConfirm({ kind: "restore", snapshot: snapshots[0] });
    const user = userEvent.setup();
    await user.type(screen.getByLabelText("Type RESTORE to confirm"), "RESTORE");
    await user.click(screen.getByRole("button", { name: "Restore" }));
    expect(await screen.findByTestId("confirm-error")).toHaveTextContent(text);
  });

  it("hands a lost session back to key entry", async () => {
    fakeWorker({ "POST /restore": { status: 401, body: { error: "session_required" } } });
    const { onSessionLost } = renderConfirm({ kind: "restore", snapshot: snapshots[0] });
    const user = userEvent.setup();
    await user.type(screen.getByLabelText("Type RESTORE to confirm"), "RESTORE");
    await user.click(screen.getByRole("button", { name: "Restore" }));
    expect(onSessionLost).toHaveBeenCalledTimes(1);
  });
});
```

`hdms-frontend/apps/recovery/src/screens/progress-screen.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@hdms/i18n";
import { POLL_MS, ProgressScreen } from "./progress-screen";
import { fakeWorker } from "@/test/worker";
import { view } from "@/test/fixtures";

function renderProgress() {
  const handlers = { onUndo: vi.fn(), onRestart: vi.fn(), onSessionLost: vi.fn() };
  render(
    <LocaleProvider locale="en">
      <ProgressScreen pollMs={10} {...handlers} />
    </LocaleProvider>,
  );
  return handlers;
}

describe("ProgressScreen", () => {
  it("polls every 2 seconds by default", () => {
    expect(POLL_MS).toBe(2000);
  });

  it("shows each step as done, in progress or waiting, then the finished restore", async () => {
    const worker = fakeWorker({
      "GET /restore": [
        { body: { restore: view({ step: "validate" }) } },
        { body: { restore: view({ phase: "completed", step: "record", canUndo: true }) } },
      ],
    });
    const { onUndo } = renderProgress();

    expect(await screen.findByTestId("step-restore_scratch")).toHaveAttribute("data-state", "done");
    expect(screen.getByTestId("step-validate")).toHaveAttribute("data-state", "current");
    expect(screen.getByTestId("step-swap")).toHaveAttribute("data-state", "pending");
    expect(screen.getByText("Checking the restored data")).toBeInTheDocument();

    expect(await screen.findByTestId("restore-done")).toHaveTextContent("Sign in with the accounts as they were");
    expect(screen.getByRole("link", { name: "Open HDMS admin" })).toHaveAttribute("href", "/admin/");

    const polls = worker.called("GET /restore").length;
    await new Promise((r) => setTimeout(r, 50));
    expect(worker.called("GET /restore").length).toBe(polls);

    await userEvent.setup().click(screen.getByRole("button", { name: "Undo this restore" }));
    expect(onUndo).toHaveBeenCalledWith("2026-09-29T17:00:00Z");
  });

  it.each([
    ["maintenance_off_failed", "may still show"],
    ["record_failed", "could not be written to the audit log"],
  ])("finishes with the %s warning", async (warning, text) => {
    fakeWorker({ "GET /restore": { body: { restore: view({ phase: "completed", step: "record", warning }) } } });
    renderProgress();
    expect(await screen.findByTestId("restore-warning")).toHaveTextContent(text);
  });

  it("says an undo is done in its own words", async () => {
    fakeWorker({ "GET /restore": { body: { restore: view({ kind: "undo", phase: "completed", step: "record" }) } } });
    renderProgress();
    expect(await screen.findByTestId("restore-done")).toHaveTextContent("Restore undone");
  });

  it.each([
    ["no_admins", "no administrator account"],
    ["interrupted", "server restarted"],
    ["copy_forward_failed", "It stopped at: Keeping backup settings and the audit log."],
  ])("explains a failure with %s and offers to pick again", async (error, text) => {
    fakeWorker({ "GET /restore": { body: { restore: view({ phase: "failed", step: "copy_forward", error }) } } });
    const { onRestart } = renderProgress();
    const failed = await screen.findByTestId("restore-failed");
    expect(failed).toHaveTextContent("Nothing was replaced");
    expect(failed).toHaveTextContent(text);
    await userEvent.setup().click(screen.getByRole("button", { name: "Pick a backup again" }));
    expect(onRestart).toHaveBeenCalledTimes(1);
  });

  it("keeps polling through a worker that does not answer", async () => {
    fakeWorker({
      "GET /restore": [
        { body: { restore: view() } },
        { status: 502, body: {} },
        { body: { restore: view({ phase: "completed", step: "record" }) } },
      ],
    });
    renderProgress();
    expect(await screen.findByTestId("restore-done")).toBeInTheDocument();
  });

  it("hands a lost session back to key entry", async () => {
    fakeWorker({ "GET /restore": [{ body: { restore: view() } }, { status: 401, body: { error: "session_required" } }] });
    const { onSessionLost } = renderProgress();
    await vi.waitFor(() => expect(onSessionLost).toHaveBeenCalledTimes(1));
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd hdms-frontend && pnpm --filter recovery test -- screens`
Expected: FAIL — `Failed to resolve import "./snapshots-screen"` (and the other two).

- [ ] **Step 3: Write the screens**

`hdms-frontend/apps/recovery/src/screens/snapshots-screen.tsx`:

```tsx
import * as React from "react";
import { cn } from "@hdms/ui";
import { useLocale } from "@hdms/i18n";
import { Message } from "@/components/message";
import { Button } from "@/components/ui/button";
import { useT } from "@/i18n";
import { asRecoveryError, recoveryApi, type RecoveryError, type RestoreView, type Snapshot } from "@/lib/api";
import { commonError } from "@/lib/errors";
import { formatAgo, formatBytes, formatSnapshotDate } from "@/lib/format";

type Loaded = { snapshots: Snapshot[]; last: RestoreView | null };

export function SnapshotsScreen({
  onPick,
  onUndo,
  onRunning,
  onSessionLost,
}: {
  onPick: (snapshot: Snapshot) => void;
  onUndo: (snapshotTakenAt: string) => void;
  onRunning: () => void;
  onSessionLost: () => void;
}) {
  const t = useT();
  const { locale } = useLocale();
  const [loaded, setLoaded] = React.useState<Loaded | null>(null);
  const [error, setError] = React.useState<RecoveryError | null>(null);

  async function load() {
    setError(null);
    try {
      // A restore already running (started from another tab, or before the
      // worker restarted) is followed, never started twice.
      const last = await recoveryApi.restore();
      if (last?.phase === "running") {
        onRunning();
        return;
      }
      setLoaded({ snapshots: await recoveryApi.snapshots(), last });
    } catch (err) {
      const e = asRecoveryError(err);
      if (e.sessionLost) {
        onSessionLost();
        return;
      }
      setError(e);
    }
  }

  // Loaded once per visit to this screen; Try again reloads.
  React.useEffect(() => {
    void load();
  }, []);

  if (error) {
    return (
      <section className="space-y-4">
        <Message tone="error" testId="snapshots-error">
          {error.code === "repository_unreadable" ? t("snapshots.unreadable") : commonError(t, error)}
        </Message>
        <Button onClick={() => void load()}>{t("common.retry")}</Button>
      </section>
    );
  }
  if (!loaded) return <p className="text-muted-foreground">{t("common.loading")}</p>;

  const { snapshots, last } = loaded;
  const now = new Date();
  return (
    <section className="space-y-6">
      <div className="space-y-1">
        <h2 className="text-lg font-semibold">{t("snapshots.heading")}</h2>
        <p className="text-sm text-muted-foreground">{t("snapshots.hint")}</p>
      </div>

      {last?.canUndo && (
        <Message tone="info" testId="last-restore">
          <p>{t("snapshots.lastRestore", { date: formatSnapshotDate(locale, last.snapshotTakenAt) })}</p>
          <Button variant="outline" className="mt-3" onClick={() => onUndo(last.snapshotTakenAt)}>
            {t("snapshots.undo")}
          </Button>
        </Message>
      )}

      {snapshots.length === 0 ? (
        <p>{t("snapshots.empty")}</p>
      ) : (
        <ul className="space-y-3">
          {snapshots.map((s, i) => (
            <li
              key={s.id}
              data-testid="snapshot"
              data-newest={i === 0 ? "true" : undefined}
              className={cn(
                "flex flex-wrap items-center justify-between gap-3 rounded-lg border p-4",
                i === 0 && "border-primary bg-primary/5",
              )}
            >
              <div className="space-y-1">
                <p className="font-semibold">
                  {t("snapshots.when", {
                    date: formatSnapshotDate(locale, s.takenAt),
                    ago: formatAgo(locale, s.takenAt, now),
                  })}
                  {i === 0 && (
                    <span className="ml-2 rounded-full bg-primary px-2 py-0.5 text-xs text-primary-foreground">
                      {t("snapshots.newest")}
                    </span>
                  )}
                </p>
                <p className="text-sm text-muted-foreground">{t("snapshots.size", { size: formatBytes(s.sizeBytes) })}</p>
              </div>
              <Button variant={i === 0 ? "default" : "outline"} onClick={() => onPick(s)}>
                {t("snapshots.choose")}
              </Button>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
```

`hdms-frontend/apps/recovery/src/screens/confirm-screen.tsx`:

```tsx
import * as React from "react";
import { useLocale } from "@hdms/i18n";
import { Message } from "@/components/message";
import { Button } from "@/components/ui/button";
import { useT, type RecoveryKey } from "@/i18n";
import { CONFIRM_WORD, asRecoveryError, recoveryApi, type RecoveryError, type Snapshot } from "@/lib/api";
import { commonError } from "@/lib/errors";
import { formatSnapshotDate } from "@/lib/format";

export type ConfirmMode = { kind: "restore"; snapshot: Snapshot } | { kind: "undo"; snapshotTakenAt: string };

const CONFIRM_ERRORS: Record<string, RecoveryKey> = {
  database_server_down: "confirm.errors.database_server_down",
  snapshot_not_found: "confirm.errors.snapshot_not_found",
  nothing_to_undo: "confirm.errors.nothing_to_undo",
};

export function ConfirmScreen({
  mode,
  onStarted,
  onBack,
  onSessionLost,
}: {
  mode: ConfirmMode;
  onStarted: () => void;
  onBack: () => void;
  onSessionLost: () => void;
}) {
  const t = useT();
  const { locale } = useLocale();
  const [typed, setTyped] = React.useState("");
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState<RecoveryError | null>(null);

  const date = formatSnapshotDate(locale, mode.kind === "restore" ? mode.snapshot.takenAt : mode.snapshotTakenAt);
  // Case does not matter to a person; the worker always receives RESTORE.
  const confirmed = typed.trim().toUpperCase() === CONFIRM_WORD;

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    if (!confirmed || busy) return;
    setBusy(true);
    setError(null);
    try {
      if (mode.kind === "restore") await recoveryApi.startRestore(mode.snapshot.id);
      else await recoveryApi.undo();
      onStarted();
    } catch (err) {
      const e = asRecoveryError(err);
      if (e.sessionLost) {
        onSessionLost();
        return;
      }
      if (e.code === "restore_running") {
        onStarted();
        return;
      }
      setError(e);
      setBusy(false);
    }
  }

  const errorKey = error ? CONFIRM_ERRORS[error.code] : undefined;
  return (
    <section className="space-y-6">
      <h2 className="text-lg font-semibold">
        {t(mode.kind === "restore" ? "confirm.heading" : "confirm.undoHeading")}
      </h2>
      <Message tone="warning">
        <p className="font-semibold">
          {mode.kind === "restore" ? t("confirm.replace", { date }) : t("confirm.undoBody", { date })}
        </p>
        <p className="mt-1">{t("confirm.kiosks")}</p>
      </Message>
      <form onSubmit={submit} className="space-y-4" noValidate>
        <div className="space-y-2">
          <label htmlFor="confirm-word" className="block text-sm font-medium">
            {t("confirm.typeLabel")}
          </label>
          <input
            id="confirm-word"
            value={typed}
            onChange={(e) => setTyped(e.target.value)}
            autoComplete="off"
            autoCapitalize="characters"
            spellCheck={false}
            className="h-12 w-full rounded-md border bg-background px-3 font-mono text-lg uppercase tracking-wider focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
          />
        </div>
        {error && (
          <Message tone="error" testId="confirm-error">
            {errorKey ? t(errorKey) : commonError(t, error)}
          </Message>
        )}
        <div className="flex flex-wrap gap-3">
          <Button type="submit" variant="destructive" size="lg" disabled={!confirmed || busy}>
            {busy ? t("confirm.starting") : t(mode.kind === "restore" ? "confirm.start" : "confirm.startUndo")}
          </Button>
          <Button variant="outline" size="lg" onClick={onBack}>
            {t("common.back")}
          </Button>
        </div>
      </form>
    </section>
  );
}
```

`hdms-frontend/apps/recovery/src/screens/progress-screen.tsx`:

```tsx
import * as React from "react";
import { CheckCircle2, Circle, Loader2 } from "lucide-react";
import { useLocale } from "@hdms/i18n";
import { Message } from "@/components/message";
import { Button } from "@/components/ui/button";
import { buttonVariants } from "@/components/ui/button-variants";
import { useT, type RecoveryKey, type Translate } from "@/i18n";
import { asRecoveryError, recoveryApi, type RecoveryError, type RestoreView, type Step } from "@/lib/api";
import { commonError } from "@/lib/errors";
import { formatSnapshotDate } from "@/lib/format";

export const POLL_MS = 2000;
/** The admin console in production (Caddy serves it at /admin/). */
export const ADMIN_URL = "/admin/";

const STEP_LABELS: Record<Step, RecoveryKey> = {
  safety_backup: "progress.steps.safety_backup",
  restore_scratch: "progress.steps.restore_scratch",
  migrate_scratch: "progress.steps.migrate_scratch",
  validate: "progress.steps.validate",
  maintenance_on: "progress.steps.maintenance_on",
  copy_forward: "progress.steps.copy_forward",
  swap: "progress.steps.swap",
  maintenance_off: "progress.steps.maintenance_off",
  record: "progress.steps.record",
};

const WARNINGS: Record<string, RecoveryKey> = {
  maintenance_off_failed: "done.warnings.maintenance_off_failed",
  record_failed: "done.warnings.record_failed",
};

/** state.go: Error is "<step>_failed", "interrupted" or "no_admins". */
function failureReason(t: Translate, code?: string): string | null {
  if (code === "interrupted") return t("failed.errors.interrupted");
  if (code === "no_admins") return t("failed.errors.no_admins");
  const step = code?.replace(/_failed$/, "") as Step | undefined;
  if (step && step in STEP_LABELS) return t("failed.errors.step", { step: t(STEP_LABELS[step]) });
  return null;
}

type StepState = "done" | "current" | "pending";

function StepList({ view }: { view: RestoreView }) {
  const t = useT();
  const current = view.steps.indexOf(view.step);
  return (
    <ol className="space-y-2">
      {view.steps.map((step, i) => {
        const state: StepState =
          view.phase === "completed" || i < current ? "done" : i === current ? "current" : "pending";
        const Icon = state === "done" ? CheckCircle2 : state === "current" ? Loader2 : Circle;
        const stateKey: RecoveryKey =
          state === "done" ? "progress.stepDone" : state === "current" ? "progress.stepCurrent" : "progress.stepPending";
        return (
          <li key={step} data-testid={`step-${step}`} data-state={state} className="flex items-center gap-3">
            <Icon
              aria-hidden="true"
              className={
                state === "done"
                  ? "size-5 text-success"
                  : state === "current"
                    ? "size-5 animate-spin text-primary"
                    : "size-5 text-muted-foreground"
              }
            />
            <span className={state === "pending" ? "text-muted-foreground" : undefined}>{t(STEP_LABELS[step])}</span>
            <span className="sr-only">{t(stateKey)}</span>
          </li>
        );
      })}
    </ol>
  );
}

export function ProgressScreen({
  pollMs = POLL_MS,
  onUndo,
  onRestart,
  onSessionLost,
}: {
  pollMs?: number;
  onUndo: (snapshotTakenAt: string) => void;
  onRestart: () => void;
  onSessionLost: () => void;
}) {
  const t = useT();
  const { locale } = useLocale();
  const [view, setView] = React.useState<RestoreView | null>(null);
  const [error, setError] = React.useState<RecoveryError | null>(null);

  // Poll until the run ends. A failed poll (the worker restarting, Caddy
  // answering 502) keeps polling; only a lost session leaves the screen.
  React.useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const poll = async () => {
      try {
        const next = await recoveryApi.restore();
        if (cancelled) return;
        setView(next);
        setError(null);
        if (next?.phase !== "running") return;
      } catch (err) {
        if (cancelled) return;
        const e = asRecoveryError(err);
        if (e.sessionLost) {
          onSessionLost();
          return;
        }
        setError(e);
      }
      timer = setTimeout(poll, pollMs);
    };
    void poll();
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [pollMs]);

  if (!view) {
    return error ? (
      <Message tone="warning">{commonError(t, error)}</Message>
    ) : (
      <p className="text-muted-foreground">{t("common.loading")}</p>
    );
  }

  const date = formatSnapshotDate(locale, view.snapshotTakenAt);

  if (view.phase === "failed") {
    const reason = failureReason(t, view.error);
    return (
      <section className="space-y-6">
        <Message tone="error" testId="restore-failed">
          <h2 className="font-semibold">{t(view.kind === "undo" ? "failed.undoHeading" : "failed.heading")}</h2>
          <p className="mt-1">{t("failed.body")}</p>
          {reason && <p className="mt-1">{reason}</p>}
        </Message>
        <Button onClick={onRestart}>{t("failed.again")}</Button>
      </section>
    );
  }

  if (view.phase === "completed") {
    const warning = view.warning ? WARNINGS[view.warning] : undefined;
    return (
      <section className="space-y-6">
        <Message tone="info" testId="restore-done">
          <h2 className="font-semibold">{t(view.kind === "undo" ? "done.undoneHeading" : "done.heading")}</h2>
          <p className="mt-1">{view.kind === "undo" ? t("done.undoneBody") : t("done.body", { date })}</p>
        </Message>
        {view.warning && (
          <Message tone="warning" testId="restore-warning">
            {warning ? t(warning) : t("common.unexpected")}
          </Message>
        )}
        <div className="flex flex-wrap gap-3">
          <a href={ADMIN_URL} className={buttonVariants({ size: "lg" })}>
            {t("done.openAdmin")}
          </a>
          {view.canUndo && (
            <Button variant="outline" size="lg" onClick={() => onUndo(view.snapshotTakenAt)}>
              {t("snapshots.undo")}
            </Button>
          )}
        </div>
      </section>
    );
  }

  return (
    <section className="space-y-6">
      <div className="space-y-1">
        <h2 className="text-lg font-semibold">{t(view.kind === "undo" ? "progress.undoHeading" : "progress.heading")}</h2>
        <p className="text-sm text-muted-foreground">{t("progress.keepOpen")}</p>
      </div>
      {error && <Message tone="warning">{commonError(t, error)}</Message>}
      <StepList view={view} />
    </section>
  );
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd hdms-frontend && pnpm --filter recovery test && pnpm --filter recovery lint && pnpm --filter recovery exec tsc -b`
Expected: PASS, lint clean (warnings from `only-export-components` on `POLL_MS`/`ADMIN_URL` are allowed constants), `tsc -b` exits 0.

- [ ] **Step 5: Commit**

```bash
git add hdms-frontend/apps/recovery/src/screens
git commit -m "feat(recovery): pick a backup, confirm, follow progress, undo"
```

---

### Task 7: Recovery app — the flow, entry point and build

**Files:**
- Create: `hdms-frontend/apps/recovery/src/App.tsx`, `hdms-frontend/apps/recovery/src/main.tsx`
- Test: `hdms-frontend/apps/recovery/src/App.test.tsx`

**Interfaces:**
- Consumes: every screen from Tasks 5–6.
- Produces: `App({ initialLocale?, pollMs? })`; `RecoveryFlow({ pollMs? })`; the built app in `apps/recovery/dist` (Task 8 copies it into the Caddy image).

- [ ] **Step 1: Write the failing test**

`hdms-frontend/apps/recovery/src/App.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { App } from "./App";
import { fakeWorker } from "@/test/worker";
import { localSource, snapshots, view, workingStatus } from "@/test/fixtures";

const KEY = "ABCD-EFGH-JKMN-PQRS-TVWX-YZ01-2345";

async function unlock(user: ReturnType<typeof userEvent.setup>) {
  await user.click(await screen.findByTestId("source-local"));
  await user.type(screen.getByLabelText("Recovery key"), KEY);
  await user.click(screen.getByRole("button", { name: "Unlock" }));
}

describe("recovery flow", () => {
  it("walks from the status page to a finished restore", async () => {
    const worker = fakeWorker({
      "GET /status": { body: workingStatus },
      "GET /sources": { body: { sources: [localSource] } },
      "POST /unlock": { body: { keysMatch: true } },
      "GET /snapshots": { body: { snapshots } },
      "POST /restore": { status: 202, body: { restore: view() } },
      "GET /restore": [
        { body: { restore: null } },
        { body: { restore: view() } },
        { body: { restore: view({ phase: "completed", step: "record", canUndo: true }) } },
      ],
    });
    const user = userEvent.setup();
    render(<App initialLocale="en" pollMs={10} />);

    await unlock(user);
    await user.click((await screen.findAllByRole("button", { name: "Restore this backup" }))[0]);
    await user.type(screen.getByLabelText("Type RESTORE to confirm"), "RESTORE");
    await user.click(screen.getByRole("button", { name: "Restore" }));

    expect(await screen.findByTestId("restore-done")).toHaveTextContent("Restored");
    expect(worker.called("POST /restore")[0].body).toEqual({ snapshotId: "snap-new", confirmation: "RESTORE" });
  });

  it("returns to key entry when the session is lost mid-restore and picks the restore up again", async () => {
    const worker = fakeWorker({
      "GET /status": { body: { ...workingStatus, restoreRunning: true } },
      "GET /sources": { body: { sources: [localSource] } },
      "POST /unlock": { body: { keysMatch: true } },
      "GET /snapshots": { body: { snapshots } },
      "GET /restore": [
        { body: { restore: view() } },
        { status: 401, body: { error: "session_required" } },
        { body: { restore: view({ step: "swap" }) } },
        { body: { restore: view({ phase: "completed", step: "record" }) } },
      ],
    });
    const user = userEvent.setup();
    render(<App initialLocale="en" pollMs={10} />);

    await unlock(user);
    expect(await screen.findByText(/recovery session ended/)).toBeInTheDocument();

    await user.type(screen.getByLabelText("Recovery key"), KEY);
    await user.click(screen.getByRole("button", { name: "Unlock" }));

    expect(await screen.findByTestId("restore-done")).toBeInTheDocument();
    expect(worker.called("POST /restore")).toHaveLength(0);
    expect(worker.called("GET /snapshots")).toHaveLength(0);
  });

  it("stops at a keys mismatch and goes back to the start", async () => {
    fakeWorker({
      "GET /status": { body: workingStatus },
      "GET /sources": { body: { sources: [localSource] } },
      "POST /unlock": { status: 409, body: { error: "keys_mismatch" } },
    });
    const user = userEvent.setup();
    render(<App initialLocale="en" />);

    await unlock(user);
    expect(await screen.findByTestId("keys-mismatch")).toHaveTextContent("install.sh --restore");
    await user.click(screen.getByRole("button", { name: "Back" }));
    expect(await screen.findByTestId("source-local")).toBeInTheDocument();
  });

  it("opens in Japanese and switches to English", async () => {
    fakeWorker({ "GET /status": { body: workingStatus }, "GET /sources": { body: { sources: [localSource] } } });
    const user = userEvent.setup();
    render(<App />);

    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("HDMS 復旧");
    await user.click(screen.getByTestId("language-toggle"));
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("HDMS recovery");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd hdms-frontend && pnpm --filter recovery test -- App`
Expected: FAIL — `Failed to resolve import "./App"`.

- [ ] **Step 3: Write the flow and entry point**

`hdms-frontend/apps/recovery/src/App.tsx`:

```tsx
import * as React from "react";
import { LocaleProvider, type Locale } from "@hdms/i18n";
import { Page } from "@/components/page";
import type { Snapshot, Source } from "@/lib/api";
import { ConfirmScreen } from "@/screens/confirm-screen";
import { MismatchScreen } from "@/screens/mismatch-screen";
import { ProgressScreen } from "@/screens/progress-screen";
import { SnapshotsScreen } from "@/screens/snapshots-screen";
import { StartScreen } from "@/screens/start-screen";
import { UnlockScreen } from "@/screens/unlock-screen";

type Stage =
  | { name: "start" }
  | { name: "unlock"; source: Source; sessionEnded: boolean }
  | { name: "mismatch" }
  | { name: "snapshots"; source: Source }
  | { name: "confirm"; source: Source; snapshot: Snapshot }
  | { name: "confirmUndo"; source: Source; snapshotTakenAt: string }
  | { name: "progress"; source: Source };

// One page, no router: the flow is linear and the worker holds the only
// state that matters (the session and the restore).
export function RecoveryFlow({ pollMs }: { pollMs?: number }) {
  const [stage, setStage] = React.useState<Stage>({ name: "start" });

  switch (stage.name) {
    case "start":
      return <StartScreen onChoose={(source) => setStage({ name: "unlock", source, sessionEnded: false })} />;
    case "unlock":
      return (
        <UnlockScreen
          source={stage.source}
          sessionEnded={stage.sessionEnded}
          onUnlocked={() => setStage({ name: "snapshots", source: stage.source })}
          onMismatch={() => setStage({ name: "mismatch" })}
          onBack={() => setStage({ name: "start" })}
        />
      );
    case "mismatch":
      return <MismatchScreen onBack={() => setStage({ name: "start" })} />;
    default: {
      // Every screen behind the key returns here when the worker forgets the
      // session (30 minutes idle, or a worker restart).
      const source = stage.source;
      const sessionLost = () => setStage({ name: "unlock", source, sessionEnded: true });
      const toProgress = () => setStage({ name: "progress", source });
      const toUndo = (snapshotTakenAt: string) => setStage({ name: "confirmUndo", source, snapshotTakenAt });
      const toSnapshots = () => setStage({ name: "snapshots", source });
      switch (stage.name) {
        case "snapshots":
          return (
            <SnapshotsScreen
              onPick={(snapshot) => setStage({ name: "confirm", source, snapshot })}
              onUndo={toUndo}
              onRunning={toProgress}
              onSessionLost={sessionLost}
            />
          );
        case "confirm":
          return (
            <ConfirmScreen
              mode={{ kind: "restore", snapshot: stage.snapshot }}
              onStarted={toProgress}
              onBack={toSnapshots}
              onSessionLost={sessionLost}
            />
          );
        case "confirmUndo":
          return (
            <ConfirmScreen
              mode={{ kind: "undo", snapshotTakenAt: stage.snapshotTakenAt }}
              onStarted={toProgress}
              onBack={toSnapshots}
              onSessionLost={sessionLost}
            />
          );
        case "progress":
          return <ProgressScreen pollMs={pollMs} onUndo={toUndo} onRestart={toSnapshots} onSessionLost={sessionLost} />;
      }
    }
  }
}

export function App({ initialLocale, pollMs }: { initialLocale?: Locale; pollMs?: number }) {
  return (
    <LocaleProvider locale={initialLocale}>
      <Page>
        <RecoveryFlow pollMs={pollMs} />
      </Page>
    </LocaleProvider>
  );
}
```

`hdms-frontend/apps/recovery/src/main.tsx`:

```tsx
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import "./index.css";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
```

- [ ] **Step 4: Run the full frontend gate**

Run: `cd hdms-frontend && pnpm --filter recovery test && pnpm -r lint && pnpm -r test && pnpm -r build && VITE_BASE_PATH=/recovery/ pnpm --filter recovery build && grep -c '/recovery/assets/' apps/recovery/dist/index.html`
Expected: all PASS; the last command prints a number ≥ 1 (assets are referenced under `/recovery/`).

- [ ] **Step 5: Commit**

```bash
git add hdms-frontend/apps/recovery/src/App.tsx hdms-frontend/apps/recovery/src/main.tsx hdms-frontend/apps/recovery/src/App.test.tsx
git commit -m "feat(recovery): one-page flow from status to restore and undo"
```

---

### Task 8: Serve `/recovery` through Caddy, dev wiring, smoke and live check

**Files:**
- Modify: `deploy/production/Caddyfile`, `deploy/production/Dockerfile.caddy`, `deploy/Caddyfile`, `docker-compose.yml` (caddy `depends_on`), `deploy/production/smoke.sh`, `Taskfile.yml` (`frontend:dev`, `frontend:dev:remote`), `hdms-frontend/apps/kiosk/vite.config.ts`
- Test: `hdms-frontend/apps/kiosk/src/pwa-kiosk.test.tsx`

**Interfaces:**
- Consumes: the worker listener on `worker:8090` (plan 3a); the built recovery app (Task 7).
- Produces: `https://<host>/recovery/` (the app), `https://<host>/recovery/api/*` (the worker), `/recovery` → `308 /recovery/`.

- [ ] **Step 1: Write the failing kiosk service-worker test**

In `hdms-frontend/apps/kiosk/src/pwa-kiosk.test.tsx`, add next to `apiResponsesAreNotCached`:

```tsx
  it("leavesOtherAppsNavigationsAlone", () => {
    // Scope is "/", so without a denylist a browser that once opened the
    // kiosk would be shown the kiosk at /admin, /staff and — when it matters
    // most — /recovery.
    const denylist: RegExp[] = pwaOptions.workbox.navigateFallbackDenylist ?? [];
    const denied = (path: string) => denylist.some((re) => re.test(path));
    for (const path of ["/recovery", "/recovery/", "/admin/backups", "/staff/devices"]) {
      expect(denied(path), path).toBe(true);
    }
    for (const path of ["/", "/index.html", "/recoveryx"]) {
      expect(denied(path), path).toBe(false);
    }
  });
```

Run: `cd hdms-frontend && pnpm --filter kiosk test -- pwa-kiosk`
Expected: FAIL — `/recovery: expected false to be true`.

- [ ] **Step 2: Add the denylist**

In `hdms-frontend/apps/kiosk/vite.config.ts`, in `pwaOptions.workbox`, after `navigateFallback: "index.html",` add:

```ts
    // The kiosk's scope is "/": without this, its fallback would answer
    // navigations to the other apps, the recovery page included.
    navigateFallbackDenylist: [/^\/(admin|staff|recovery)(\/|$)/],
```

Run: `cd hdms-frontend && pnpm --filter kiosk test && pnpm --filter kiosk build`
Expected: PASS, build succeeds.

- [ ] **Step 3: Route `/recovery` in production**

In `deploy/production/Caddyfile`, insert before `# Admin console at /admin`:

```
	# Recovery page (plan 3b). The worker answers /recovery/api/* whatever
	# state the API and the database are in. Only /recovery/api/* goes to
	# the worker — never /internal/*, which is the API's alone. The worker's
	# unlock rate limit keys on the first X-Forwarded-For hop, so it is
	# replaced with the TCP peer here exactly as for the API (THR-01).
	handle /recovery/api/* {
		reverse_proxy worker:8090 {
			header_up X-Forwarded-For {http.request.remote.host}
			header_up X-Real-IP {http.request.remote.host}
		}
	}

	redir /recovery /recovery/ 308

	# Recovery app at /recovery (built with VITE_BASE_PATH=/recovery/).
	handle /recovery/* {
		root * /srv/recovery
		uri strip_prefix /recovery
		try_files {path} /index.html
		file_server
	}
```

In `deploy/production/Dockerfile.caddy`:
- change the header comment's "compiles the three Vite apps with their production base paths (/ kiosk, /admin console, /staff PWA)" to "compiles the four Vite apps with their production base paths (/ kiosk, /admin console, /staff PWA, /recovery page)"
- after `RUN VITE_BASE_PATH=/staff/ pnpm --filter staff build` add:

```dockerfile
# Recovery page at /recovery: subpath base; it calls only /recovery/api.
RUN VITE_BASE_PATH=/recovery/ pnpm --filter recovery build
```

- after `COPY --from=frontend-builder /src/hdms-frontend/apps/staff/dist /srv/staff` add:

```dockerfile
COPY --from=frontend-builder /src/hdms-frontend/apps/recovery/dist /srv/recovery
```

Check the adapted config (no certificates needed for `adapt`):

Run:
```bash
docker run --rm -v "$PWD/deploy/production/Caddyfile:/etc/caddy/Caddyfile:ro" \
  caddy:2-alpine@sha256:ad27e531c8b286ff153c0e6e16587a1583e4111bb58c4d83bd73d6d3ef0a0ce1 \
  caddy adapt --config /etc/caddy/Caddyfile --adapter caddyfile > "$TMPDIR/caddy-prod.json" \
  && grep -o '"dial":"worker:8090"' "$TMPDIR/caddy-prod.json" | wc -l \
  && grep -o '"X-Forwarded-For":\["{http.request.remote.host}"\]' "$TMPDIR/caddy-prod.json" | wc -l \
  && grep -c '/internal' "$TMPDIR/caddy-prod.json"
```
Expected: `1` (one route to the worker), `2` (API and worker both overwrite the header), `0` (nothing routes `/internal`).

- [ ] **Step 4: Route `/recovery/api` in dev and run the recovery dev server**

Replace the body of `https://localhost:8443 { … }` in `deploy/Caddyfile` after the `handle @options { … }` block — i.e. the bare `reverse_proxy api:8443 { … }` — with:

```
	# Recovery API (plan 3b): straight to the worker, as in production. The
	# recovery Vite server (:5176) proxies /recovery/api here.
	handle /recovery/api/* {
		reverse_proxy worker:8090 {
			header_up X-Forwarded-For {http.request.remote.host}
		}
	}

	handle {
		reverse_proxy api:8443 {
			transport http {
				tls_insecure_skip_verify
			}
		}
	}
```

In `docker-compose.yml`, change the `caddy` service's

```yaml
    depends_on:
      - api
```

to

```yaml
    depends_on:
      - api
      - worker
```

In `Taskfile.yml`, in both `frontend:dev` and `frontend:dev:remote`, change `--filter kiosk --filter admin --filter staff` to `--filter kiosk --filter admin --filter staff --filter recovery`, and change the `frontend:dev` description to `Run the Vite dev servers (kiosk, admin, staff, recovery) over HTTPS.`

- [ ] **Step 5: Smoke-check the recovery route**

In `deploy/production/smoke.sh`, update the header comment's first line to `# Production smoke check (Phase 5.3a): /v1/healthz, /v1/readyz, /recovery, a login and a scan.` and insert after the `pass "GET /v1/readyz"` line:

```sh
recovery=$(curl -ksS -m 10 "${BASE}/recovery/api/status") || fail "GET /recovery/api/status unreachable"
echo "${recovery}" | grep -q '"database"' || fail "/recovery/api/status body unexpected: ${recovery}"
pass "GET /recovery/api/status"

curl -ksS -m 10 "${BASE}/recovery/" | grep -q '<title>HDMS Recovery</title>' || fail "/recovery/ does not serve the recovery page"
pass "GET /recovery/"
```

Run: `shellcheck deploy/production/smoke.sh`
Expected: no output.

- [ ] **Step 6: Live check on the dev stack**

From the repository root, with the dev stack on this branch (`docker compose up -d --build`; if running from a worktree, copy main's gitignored `certs/` in and point `HDMS_BACKUP_NAS_HOST_PATH` at main's `.dev-backups`), and `cd hdms-frontend && pnpm --filter recovery dev` in another terminal:

1. `curl -ks https://localhost:8443/recovery/api/status` — JSON with `"database":"working"`.
2. Header spoofing (Review Focus 1). Make a checksum-valid but wrong key and send it with a forged header:
   ```bash
   mkdir -p hdms-backend/cmd/zz-keygen && cat > hdms-backend/cmd/zz-keygen/main.go <<'EOF'
   package main

   import (
   	"fmt"

   	"github.com/hito-hospital/hdms/internal/platform/backup"
   )

   func main() {
   	k, err := backup.NewRecoveryKey()
   	if err != nil {
   		panic(err)
   	}
   	fmt.Println(k.String())
   }
   EOF
   KEY=$(cd hdms-backend && go run ./cmd/zz-keygen) && rm -rf hdms-backend/cmd/zz-keygen
   curl -ks -X POST -H 'Content-Type: application/json' -H 'X-Forwarded-For: 203.0.113.9' \
     -d "{\"source\":\"local\",\"key\":\"$KEY\"}" https://localhost:8443/recovery/api/unlock
   docker compose logs worker --since 1m | grep 'unlock attempt'
   ```
   Expected: `{"error":"key_wrong"}`, and the log line's `ip` is the Docker gateway address, **not** `203.0.113.9`. (`git status` shows no `zz-keygen` left behind.)
3. Browser, `https://localhost:5176/`: the status card says the database is working; "This server" is listed; a mistyped key shows the typo message and keeps the key; the language toggle switches both ways.
4. Maintenance notices (Review Focus 3, 4): `docker compose exec db psql -U hdms -d hdms -c "UPDATE system_state SET maintenance = true, maintenance_reason = 'restore', maintenance_since = now() WHERE id = 1"`. Then: the admin console at `https://localhost:5174/dashboard` shows the notice with **Open Backups**, and `/backups` still works; the staff app at `https://localhost:5175/` shows the notice and stays on its URL; on a paired kiosk at `https://localhost:5173/`, a scan (attendant manual entry) brings up the maintenance screen, not "Reconnecting". Switch it off (`… SET maintenance = false, maintenance_reason = NULL, maintenance_since = NULL …`); within 15 seconds every app returns by itself.
5. Production image: `docker compose -f deploy/production/compose.yaml build caddy` succeeds, and `docker run --rm --entrypoint ls <built image> /srv/recovery` lists `index.html` and `assets`.
6. Full restore through the page needs the dev installation's recovery key, which exists only on its printed sheet. If the user has it (or creates a fresh one in the console), restore the newest snapshot of "This server" and undo it; otherwise record that this step is left for the human.

Return the dev stack to `main` afterwards if it was switched to a worktree.

- [ ] **Step 7: Commit**

```bash
git add deploy/production/Caddyfile deploy/production/Dockerfile.caddy deploy/Caddyfile docker-compose.yml \
  deploy/production/smoke.sh Taskfile.yml hdms-frontend/apps/kiosk/vite.config.ts hdms-frontend/apps/kiosk/src/pwa-kiosk.test.tsx
git commit -m "feat(deploy): serve /recovery and route /recovery/api to the worker"
```

---

## Self-review notes

- **Spec coverage.** Recovery page flow 1–8 (status, sources incl. configured destinations by name, key entry with rate-limit and mismatch messages, plain-date snapshot list with newest highlighted and size, `RESTORE` confirmation with the paper-register line, 2-second progress, done with sign-in note and `/admin` link, undo while the previous database is kept): Tasks 5–7. App location, own `en`/`ja` catalogues and the `no-literals` rule: Task 5. Caddy `/recovery` static and `/recovery/api/*` → `worker:8090`, `/internal` never routed: Task 8. Kiosk notice, no enqueue, replay holds, 15-second re-check, return to idle, glyph rule: Task 3. Staff full-page notice with 15-second re-check: Task 4. Admin notice on every route except `/backups`: Task 4. Frontend tests for each recovery screen including rate limit and mismatch: Tasks 5–7.
- **Left for plan 4 and humans:** the runbook that the "database server is not running" and mismatch messages point to; native-speaker review of the Japanese recovery text; the staging drill (drop the database, restore through `/recovery`, sign in).

## Changes made during execution

- **Task 4, staff (plan bug).** Rethrowing from the auth guard during maintenance made the router replace the whole app with its error screen (the root unmounted, so the notice and its re-check vanished), and `router.invalidate()` does not retry a match that failed in `beforeLoad`. Instead the guard now awaits `maintenanceEnded()` (new in `@hdms/ui`, Task 2's store) and asks `/v1/staff/me` again; because the router renders nothing while a guard is pending, the staff notice moved out of `routes/root.tsx` into `MaintenanceOverlay`, a fixed full-screen notice rendered beside `RouterProvider` in `App.tsx`. `routes/root.tsx` is unchanged.
- **Task 4, admin test (plan bug).** The generated client builds `new Request("/v1/…")`; Node's `Request` rejects a relative URL, so `fetch` was never called. The first admin test sets `client.setConfig({ baseUrl: "http://localhost/v1" })`.
- **Task 5 (plan bug).** In `src/lib/api.test.ts`, `recoveryApi.snapshots().catch(asRecoveryError)` types as `Snapshot[] | RecoveryError`, which `tsc -b` rejects; the test uses `.then(() => { throw … }, asRecoveryError)` instead. `StartScreen` resets its state only on a user-triggered reload, not at the top of the effect's `load`.
- **Task 6 (plan bugs).** The progress test that checks step states raced its own 10 ms poll (the screen had already moved to "Restored"); it now polls at 300 ms. `SnapshotsScreen` and `ProgressScreen` keep the flow's callbacks in refs so their effects do not depend on callbacks that change every render (`exhaustive-deps`).
