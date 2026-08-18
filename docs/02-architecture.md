# 02 — Architecture

## Shape of the system

```
┌──────────────────┐        ┌──────────────────┐
│  Kiosk PWA       │        │  Admin console   │
│  (iPad, Safari   │        │  (desktop React) │
│   standalone)    │        │                  │
└────────┬─────────┘        └────────┬─────────┘
         │  HTTPS/JSON + SSE          │
         └─────────────┬──────────────┘
                       ▼
         ┌─────────────────────────────┐
         │   HDMS API (Go monolith)    │
         │  ┌───────────────────────┐  │
         │  │ HTTP layer (generated │  │
         │  │ from OpenAPI spec)    │  │
         │  ├───────────────────────┤  │
         │  │ checkout  (orchestr.) │  │
         │  ├────────┬────────┬─────┤  │
         │  │identity│catalog │lending│ │
         │  ├────────┴────────┴─────┤  │
         │  │ credentials           │  │
         │  ├───────────────────────┤  │
         │  │ audit · notification  │  │
         │  ├───────────────────────┤  │
         │  │ platform: db, http,   │  │
         │  │ auth, events, config  │  │
         │  └───────────────────────┘  │
         └──────────────┬──────────────┘
                        ▼
                 ┌─────────────┐
                 │ PostgreSQL  │
                 └─────────────┘
```

One deployable binary, one database, several strictly separated modules.

## Why a modular monolith

At 5–10 transactions a day, microservices would be pure cost: distributed
transactions, network failure modes, and five deployment pipelines to protect a
paper register. But the requirement is explicitly *loosely coupled, reusable,
scalable*. A modular monolith delivers exactly that: service-grade internal
boundaries, single-process simplicity, and a credible extraction path if the
hospital later wants, say, an independent identity service backed by HR.

See [ADR-0001](adr/0001-modular-monolith-go.md).

## Module map

Each module owns its tables, its domain logic, and a narrow public interface.

| Module | Owns | Public surface (`…api` package) |
|---|---|---|
| `identity` | Staff users, departments, roles, status | `LookupUser`, `RegisterUser`, `SuspendUser`, `UserSummary`. `RegisterUser` is reachable only from admin-authenticated handlers — never from the kiosk (FR-45) |
| `catalog` | Devices, categories, condition, lifecycle status | `LookupDevice`, `SetStatus`, `DeviceSummary` |
| `credentials` | Tokens, symbology kinds, issue/revoke/reprint, resolution | `Resolve(token) → SubjectRef`, `Issue`, `Bind`, `Revoke`, `Reissue` |
| `lending` | Loans, custody, overdue, history | `OpenLoan`, `CloseLoan`, `OpenLoansFor`, `HolderOf` |
| `checkout` | The scan session state machine; orchestrates the four modules above | `Scan(sessionID, token)`, `ReturnLoan`, `Close`, `Cancel`. It borrows and returns only — it has no user-creating operation |
| `audit` | Append-only record of every consequential action | `Record(event)` |
| `notification` | Overdue reminders, digests (Phase 6) | `Enqueue(notice)` |

### Dependency rules

```
checkout ──▶ identity
         ──▶ catalog
         ──▶ credentials
         ──▶ lending

lending  ──▶ (nothing; receives IDs, not entities)
identity, catalog, credentials ──▶ (nothing but platform)

every module ──▶ platform/*
every module ──▶ audit (write-only)
```

- **Dependencies point one way.** `checkout` is the only orchestrator; nothing
  depends on `checkout`.
- **No module reads another module's tables.** Ever. `lending` stores a
  `device_id` and a `user_id` and knows nothing else about them; if it needs a
  device name it asks `catalog` through the interface.
- **No shared "models" package.** Each module defines its own types. Crossing a
  boundary means mapping to the other module's DTO. This is the small, deliberate
  cost that keeps extraction possible.
- **Communication is synchronous via interfaces** for reads and commands, and
  **asynchronous via domain events** for reactions (audit, notifications).

### Enforcing the boundaries

Rules that only live in a document get violated in week three. These are enforced
mechanically:

1. **Package nesting.** A module lives at
   `internal/modules/<name>/` with internals under
   `internal/modules/<name>/internal/…`. Go's `internal` rule makes those
   packages *uncompilable* from other modules — the compiler is the guard.
2. **`depguard`** rules in `golangci-lint` deny any import path matching
   `modules/(?!<self>)` outside the allowed list, and deny `checkout` from being
   imported at all.
3. **CI gate.** Boundary violations fail the build in Phase 0, before there is
   any code to grandfather in.

### Directory layout (`hdms-backend`)

```
hdms-backend/
├── cmd/
│   ├── hdms-api/main.go          # server entrypoint
│   └── hdms-cli/main.go          # import, seed, admin bootstrap, label export
├── api/
│   └── openapi.yaml              # source of truth for the HTTP contract
├── internal/
│   ├── modules/
│   │   ├── identity/
│   │   │   ├── identityapi/      # PUBLIC: interfaces + DTOs, importable
│   │   │   ├── internal/store/   # sqlc-generated queries, private
│   │   │   ├── internal/domain/  # entities, invariants, private
│   │   │   └── module.go         # wiring: New(deps) returns identityapi.Service
│   │   ├── catalog/
│   │   ├── credentials/
│   │   ├── lending/
│   │   ├── checkout/
│   │   ├── audit/
│   │   └── notification/
│   └── platform/
│       ├── config/               # env-driven configuration
│       ├── db/                   # pgx pool, tx manager, migration runner
│       ├── httpx/                # middleware, problem+json, request IDs
│       ├── auth/                 # sessions, kiosk tokens, RBAC
│       ├── events/               # in-process bus + transactional outbox
│       ├── ids/                  # ULID/UUIDv7 generation
│       ├── tokens/               # credential token format + checksum
│       └── observability/        # slog, OpenTelemetry, metrics
├── migrations/                   # goose SQL migrations
├── queries/                      # sqlc .sql inputs, one dir per module
└── test/                         # integration + e2e helpers, fixtures
```

### Composition root

`cmd/hdms-api/main.go` is the only place that knows every module exists. It
constructs each module with its dependencies expressed as interfaces and hands
the assembled handler set to the HTTP server. There is no DI container and no
reflection-based wiring — roughly 60 lines of explicit constructor calls, which
is also the clearest possible documentation of the dependency graph.

```go
pool   := db.MustOpen(cfg.DatabaseURL)
bus    := events.NewBus()
audit  := auditmod.New(pool, bus)
ident  := identity.New(pool, audit)
cat    := catalog.New(pool, audit)
creds  := credentials.New(pool, cfg.TokenPepper, audit)
lend   := lending.New(pool, audit, bus)
check  := checkout.New(checkout.Deps{
    Users: ident, Devices: cat, Credentials: creds, Loans: lend,
    Clock: clock.System, SessionTTL: cfg.SessionTTL,
})
```

`checkout.Deps` names *interfaces defined by checkout itself* — the consumer
declares what it needs, the providers happen to satisfy it. That inversion is
what makes each module independently testable with fakes and independently
extractable later.

## Transactions across modules

The awkward part of any modular monolith. A borrow touches `lending` (insert a
loan) and `catalog` (set device status) and `audit` — and must be atomic.

**Approach:** a `platform/db` transaction manager passes a `db.Tx` through the
context. Module methods accept the ambient transaction if one exists and open
their own otherwise. `checkout` opens the transaction; `lending` and `catalog`
enlist in it.

```go
err := tx.Do(ctx, func(ctx context.Context) error {
    loan, err := c.loans.Open(ctx, deviceID, userID, dueAt)
    if err != nil { return err }
    if err := c.devices.SetStatus(ctx, deviceID, catalogapi.StatusOnLoan); err != nil { return err }
    return c.events.Publish(ctx, lendingapi.LoanOpened{...}) // outbox row, same tx
})
```

This keeps atomicity without leaking SQL across boundaries. If a module is ever
extracted, the transaction becomes a saga — and the outbox is already there to
carry it.

Events are written to an **outbox table inside the same transaction** and
dispatched after commit by a background poller. Nothing is published for a
transaction that rolled back, and no listener can fail a borrow.

## Technology choices

| Concern | Choice | Why |
|---|---|---|
| Language | **Go 1.26** | User's choice, and correct: single static binary, trivial on-prem deployment, excellent concurrency for SSE, strong stdlib HTTP |
| HTTP routing | **`net/http` stdlib mux** | Since Go 1.22 the stdlib mux does method+path patterns and wildcards. No framework dependency to maintain for ~40 endpoints |
| Database | **PostgreSQL 18** | Partial unique indexes (the double-borrow guard), `jsonb` for audit payloads, `generated` columns, robust backups, ubiquitous ops knowledge |
| DB driver | **pgx v5** | Native protocol, best-in-class performance, proper Postgres type support |
| Query layer | **sqlc** | Writes SQL, generates type-safe Go. No ORM magic, no N+1 surprises, migrations and queries reviewable as SQL |
| Migrations | **goose** | Embeddable in the binary (`//go:embed migrations`), so deploy = run binary. Plain SQL up/down |
| API contract | **OpenAPI 3.1 + oapi-codegen** | Spec-first: generates Go server interfaces and the TypeScript client. Contract drift becomes a compile error ([ADR-0006](adr/0006-spec-first-openapi.md)) |
| Logging | **`log/slog`** | Stdlib structured logging; no dependency |
| Tracing/metrics | **OpenTelemetry** | Vendor-neutral; export to whatever the hospital runs, or a local Grafana stack |
| Errors | **RFC 9457 `application/problem+json`** | Machine-readable typed errors — the kiosk needs to distinguish "device held by someone else" from "network down" |
| Testing | **`testing` + testcontainers-go** | Integration tests against a real Postgres, not a mock |
| Frontend | **React 19 + TypeScript + Vite** | User's choice; Vite for fast builds and first-class PWA plugin |
| Frontend routing | **TanStack Router** | Type-safe routes; small surface for a 6-screen kiosk |
| Server state | **TanStack Query** | Caching, retries, and offline mutation queues (needed in Phase 5) |
| Kiosk flow state | **XState v5** | The scan session *is* a state machine; encoding it as one makes the flow inspectable and the backend/frontend machines verifiably parallel ([ADR-0004](adr/0004-scan-session-state-machine.md)) |
| Styling / components | **Tailwind CSS v4 + shadcn/ui** | Copy-in components we own and can resize for touch; no runtime component library lock-in |
| Barcode decode (camera) | **`barcode-detector` ponyfill (zxing-wasm)** | Uses the native `BarcodeDetector` where present, WASM elsewhere. Safari has no native support, so the polyfill is required on iPad |
| Barcode render (labels) | **`bwip-js`** | Renders QR, Code 128, Data Matrix and 100+ symbologies client-side to canvas/SVG; label preview is WYSIWYG with what prints |
| Monorepo tooling | **pnpm workspaces** | Two apps sharing a generated API client and a UI package, without publishing |

### Deliberately not used

- **No ORM (GORM/ent).** The schema is small and the invariants are SQL-shaped
  (partial unique indexes, `FOR UPDATE` locks). sqlc keeps SQL visible.
- **No gRPC internally.** Modules are in-process; interfaces are cheaper than
  protobuf codegen.
- **No Redis.** Scan sessions live in Postgres with a TTL column. One fewer
  moving part to back up and monitor at this scale.
- **No Kubernetes.** A single Docker Compose stack or a systemd unit on a
  hospital VM is right-sized and operable by the existing IT team.
- **No Next.js.** There is no SEO or SSR requirement; the kiosk must work as an
  offline-capable SPA, which Vite does more simply.

## Frontend structure (`hdms-frontend`)

```
hdms-frontend/
├── pnpm-workspace.yaml
├── apps/
│   ├── kiosk/        # borrower-facing PWA (iPad)
│   └── admin/        # management console (desktop)
└── packages/
    ├── api-client/   # GENERATED from ../../hdms-backend/api/openapi.yaml
    ├── scan/         # ScanSource abstraction + HID / camera / manual sources
    ├── ui/           # shared design tokens + shadcn components
    └── domain/       # shared TS types, token parser (parity-tested with Go)
```

Two apps, not one, because their non-functional needs diverge: the kiosk is an
installed PWA with a service worker, an offline queue and locked navigation; the
admin is an ordinary authenticated SPA. Sharing one bundle would force the
kiosk to carry admin code and the admin to inherit kiosk caching.

The `scan` package is where FR-64 is satisfied — see
[07-kiosk-app.md](07-kiosk-app.md).

## Scaling path

Not needed now; documented so the design does not have to be revisited.

| If… | Then… |
|---|---|
| More kiosks (5–20) | No change. Single instance handles orders of magnitude more |
| Other departments adopt it (500+ devices) | Add read indexes; still one instance |
| Multi-site hospital group | Add a `tenant_id` column set; the module boundaries already prevent cross-tenant leakage from spreading |
| HR system becomes the source of truth for staff | Replace `identity`'s internals with an HR adapter. Its interface, and therefore every caller, is unchanged |
| Reporting load competes with the kiosk | Add a Postgres read replica; point admin queries at it |
| A module truly must be its own service | Its `…api` interface becomes an HTTP/gRPC client; the outbox already carries its events |
