# ADR-0001 — Modular monolith in Go

**Status:** Accepted · **Date:** 2026-08-18

## Context

The requirement asks for a system that is "loosely coupled, reusable, modern and
scalable". The actual load is 5–10 transactions per day over ~50 devices. These
two facts pull in opposite directions if "scalable" is read as "distributed".

## Decision

A single Go binary containing strictly-bounded modules — `identity`, `catalog`,
`credentials`, `lending`, `checkout`, `audit`, `notification` — each owning its
tables and exposing only an interface package.

Boundaries are enforced mechanically, not by convention: module internals live
under Go's `internal/` so the compiler rejects cross-module access, and
`depguard` in `golangci-lint` denies the remaining import paths. CI fails on a
violation.

## Consequences

**Good**
- One binary to deploy, monitor and back up — matched to a hospital IT team's
  capacity.
- Cross-module operations (borrow touches `lending`, `catalog` and `audit`) are a
  single database transaction, not a distributed saga.
- Modules are independently testable with fakes, and independently extractable:
  the interface stays, only the implementation moves behind a network call.
- Local development is one `task dev`.

**Bad**
- Boundary discipline requires ongoing vigilance; the lint is what makes it stick.
- Mapping DTOs across boundaries is real, if small, ceremony.
- One deployment unit means a change anywhere redeploys everything. At this
  release cadence that is a non-issue.

## Alternatives

- **Microservices** — rejected. Five pipelines and distributed transactions to
  protect a paper register is cost without benefit at this scale.
- **Layered monolith (controllers/services/repositories)** — rejected. It scales
  by technical layer, not by domain, and produces a shared `models` package that
  couples everything to everything. Extraction later becomes a rewrite.
