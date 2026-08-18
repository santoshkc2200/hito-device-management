# ADR-0006 — Spec-first OpenAPI with generated server and client

**Status:** Accepted · **Date:** 2026-08-18

## Context

One Go API serves two React applications, developed in parallel by the same small
team. The classic failure is silent contract drift: the handler renames a field,
the kiosk breaks in the corridor a week later.

## Decision

`hdms-backend/api/openapi.yaml` is the source of truth. `oapi-codegen` produces
Go server interfaces and types; `openapi-ts` produces the TypeScript client and
Zod schemas into `packages/api-client`. Generated code is committed, and CI fails
if `task generate` produces a diff.

## Consequences

**Good**
- A contract change becomes a **compile error** in Go and in both frontends,
  immediately, rather than a runtime failure later.
- Phases 3 and 4 proceed in parallel against a frozen, executable contract.
- The spec doubles as the API documentation, and cannot go stale.
- Request/response validation in test mode catches shape drift automatically.

**Bad**
- Editing YAML before writing code is friction, particularly early.
- Generated code in git creates review noise — accepted for reproducible builds.
- The generators occasionally produce awkward Go types for complex unions;
  mitigated by keeping schemas simple and discriminated.

## Alternatives

- **Code-first with generated spec** — rejected. The spec becomes a lagging
  artefact and the frontends still have to trust it.
- **Hand-written client** — rejected. It is exactly the drift problem, plus
  ongoing manual work.
- **tRPC / gRPC-Web** — rejected. Excellent type safety, but it couples the
  frontends to a Go-specific transport and forecloses a plain HTTP integration
  the hospital may later want.
