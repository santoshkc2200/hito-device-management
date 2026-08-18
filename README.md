# HDMS — Hito Device Management System

Hospital device lending: staff borrow and return shared equipment (laptops,
pendrives, projectors, …) by scanning a barcode on the device and a barcode
on their card at an iPad kiosk. Full design in [`docs/`](docs/README.md);
delivery plan in [`docs/phases/`](docs/phases).

```
hito-device-management/
├── docs/                 # product, architecture, domain model, phase plans
├── hdms-backend/         # Go modular monolith (API)
├── hdms-frontend/        # pnpm workspace: kiosk app, admin app, shared packages
├── deploy/               # Caddy config for local TLS termination
├── certs/                # mkcert-issued local dev certificate (gitignored)
└── docker-compose.yml    # Postgres, Caddy, API for local dev
```

## Prerequisites

- [Go](https://go.dev) 1.26+
- [Node.js](https://nodejs.org) 24+ and [pnpm](https://pnpm.io) 10+
- [Docker](https://www.docker.com) with Compose v2
- [Task](https://taskfile.dev) (`brew install go-task`)
- [mkcert](https://github.com/FiloSottile/mkcert) (`brew install mkcert`) —
  the camera barcode fallback refuses to run over plain HTTP, so local dev
  needs a real, browser-trusted TLS certificate
- [golangci-lint](https://golangci-lint.run) (`brew install golangci-lint`)
- `oapi-codegen` and `goose`: `go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest && go install github.com/pressly/goose/v3/cmd/goose@latest`

## Quickstart

```sh
task dev
```

This creates `.env` from `.env.example` on first run, issues a local TLS
certificate via mkcert, brings up Postgres + Caddy + the API in Docker
(migrations run automatically at API startup), and starts both frontend dev
servers natively for fast HMR:

| Service | URL |
|---|---|
| API (via Caddy) | `https://localhost:8443/v1/healthz` |
| Kiosk app | `https://localhost:5173` |
| Admin app | `https://localhost:5174` |

The very first time, `mkcert -install` needs an interactive OS keychain
approval — run it yourself once (`task certs`) if `task dev` reports the
certificate isn't trusted yet.

## Common tasks

```sh
task test              # backend + frontend unit tests
task test:integration  # backend integration tests against real Postgres (testcontainers)
task e2e               # Playwright smoke tests for both apps
task lint               # golangci-lint (incl. the module-boundary guard) + frontend lint
task generate           # regenerate the Go server interfaces + TS client from api/openapi.yaml
task build              # build the API binary + both frontend apps
task down               # stop the Docker Compose stack
```

Run `task --list` for everything.

## Architecture

One Go binary, one Postgres database, seven strictly-bounded modules
(`identity`, `catalog`, `credentials`, `lending`, `checkout`, `audit`,
`notification`) — a modular monolith, not microservices, because the
transaction volume doesn't justify the operational cost. The module
boundary is enforced two ways at once: Go's own `internal/` visibility
rule, and a `depguard` lint rule that's proven to fail on a real violation
(see `hdms-backend/.golangci.yml`). Full rationale in
[`docs/02-architecture.md`](docs/02-architecture.md) and the
[ADRs](docs/adr).

## Contract-first API

`hdms-backend/api/openapi.yaml` is the single source of truth for the HTTP
contract. Neither the Go server interfaces nor the TypeScript client are
hand-edited — both are generated from it by `task generate`, and CI fails
the build if that command produces a diff.
