# Phase 0 — Foundations

**Goal:** a running skeleton with every boundary, gate and pipeline already in
place, so that Phase 1 writes features rather than infrastructure.
**Duration:** ~1 week · **Depends on:** nothing

The temptation is to skip this and start on devices. Resist it: the boundary
enforcement, the codegen pipeline and the TLS/certificate work are all far
cheaper now than retrofitted, and the last one has bitten every kiosk project
that left it to the end.

## Tasks

### 0.1 Repository and tooling
- [ ] `git init` at the repo root; `.gitignore`, `.editorconfig`, `README.md`
- [ ] `hdms-backend`: `go mod init github.com/hito-hospital/hdms` (Go 1.26)
- [ ] `hdms-frontend`: pnpm workspace with `apps/kiosk`, `apps/admin`,
      `packages/{api-client,scan,ui,domain}`
- [ ] `Taskfile.yml` at the root: `dev`, `test`, `lint`, `generate`, `migrate`,
      `seed`, `e2e`
- [ ] Conventional commits + a PR template referencing the requirement IDs

### 0.2 Local environment
- [ ] `docker-compose.yml`: PostgreSQL 18, Caddy, and the API
- [ ] `.env.example` with every variable documented
- [ ] `task dev` brings up the database, runs migrations, starts the API with
      live reload, and starts both Vite dev servers
- [ ] **Local TLS with a trusted development certificate** (mkcert), because the
      camera fallback cannot be developed over plain HTTP

### 0.3 Database foundation
- [ ] `platform/db`: pgx v5 pool, health check, transaction manager passing
      `db.Tx` through `context`
- [ ] goose wired with `//go:embed migrations`, run on startup behind a Postgres
      advisory lock
- [ ] Migration `0001_init.sql`: extensions, enums, `outbox`, `audit_events`,
      `kiosks`, `admin_accounts`
- [ ] sqlc configured with one output package per module
- [ ] `testdb` helper: testcontainers-go, migrated schema, per-test isolation via
      template databases (fast) rather than re-migrating each time

### 0.4 HTTP foundation
- [ ] `cmd/hdms-api` with stdlib `net/http` mux, graceful shutdown, timeouts
- [ ] Middleware chain: request ID → structured logging → recovery → CORS →
      auth → rate limit → tracing
- [ ] RFC 9457 problem+json error rendering, with the registered type list from
      [06](../06-api-contract.md)
- [ ] `/v1/healthz` and `/v1/readyz`
- [ ] `platform/config` — environment-driven, fails fast and loudly on a missing
      required variable

### 0.5 Contract pipeline
- [ ] `api/openapi.yaml` skeleton with health endpoints and shared schemas
- [ ] `oapi-codegen` → Go server interfaces and types
- [ ] `openapi-ts` → `packages/api-client` (typed client + Zod schemas)
- [ ] `task generate` runs both; CI asserts the working tree is clean afterwards

### 0.6 Module boundary enforcement ★
- [ ] Create the seven module directories with the
      `modules/<name>/{<name>api,internal/,module.go}` layout
- [ ] `golangci-lint` with `depguard` rules denying cross-module internal imports
      and denying any import of `checkout`
- [ ] **A deliberately-violating branch proves the lint fails**, then is deleted.
      An unverified guard is not a guard

### 0.7 Frontend skeletons
- [ ] Vite + React 19 + TS for both apps
- [ ] Tailwind v4, shadcn/ui initialised, shared tokens in `packages/ui`
- [ ] TanStack Router + Query wired
- [ ] Kiosk: PWA manifest, service worker via `vite-plugin-pwa`, standalone
      display, portrait+landscape
- [ ] Both apps call `/v1/healthz` through the **generated** client — proving the
      contract pipeline end to end
- [ ] Vitest + Testing Library + Playwright configured with one smoke test each

### 0.8 Observability
- [ ] `log/slog` JSON handler; request-scoped logger
- [ ] OpenTelemetry tracing with an OTLP exporter (console in dev)
- [ ] `/metrics` in Prometheus format
- [ ] Sensitive-field denylist in the logging middleware, with the test that
      asserts a token never reaches the log

### 0.9 CI
- [ ] GitHub Actions: lint, generate-check, unit, integration, build
- [ ] Postgres service container for integration tests
- [ ] Branch protection: no merge without green CI

### 0.10 Decision records
- [ ] Write ADR-0001 through ADR-0007 (see `docs/adr/`)

## Deliverables

- Repo with backend, frontend workspace, compose stack, CI
- `task dev` → API on `https://localhost:8443`, kiosk and admin dev servers
- One typed round trip from each frontend to the API
- Boundary lint proven to fail on a real violation
- Seven ADRs

## Exit criteria

- [ ] A fresh clone reaches a working `task dev` following only the README, on a
      machine that has never seen the project
- [ ] `task test` green, including one testcontainers integration test
- [ ] `task generate` produces no diff on a clean tree
- [ ] `golangci-lint run` passes and demonstrably rejects a cross-module import
- [ ] Both frontends load over HTTPS with a certificate the browser trusts
- [ ] CI green on `main`

## Risks

| Risk | Mitigation |
|---|---|
| testcontainers is slow on macOS and erodes the habit of running tests | Template-database cloning per test; keep the container alive across a package's tests |
| Boundary lint feels like friction and gets disabled in week three | Prove its value on day one with a real violation; document the extraction rationale in ADR-0001 |
| Certificate setup gets deferred "until deployment" | It is an exit criterion here precisely because the camera path depends on it |
