# Security Findings Register (Phase 5.2e)

**Owner:** Santosh KC  
**Date:** 2026-09-18  
**Parent:** [5.2 Security Review](./5.2-security-review.md)

---

## 1. Automated Scan Summary

Counts below are from the scans as re-run on 2026-09-18 against the current tree,
after the fixes recorded in section 2.

| Tool | Target | Findings | Status |
|---|---|---|---|
| `govulncheck ./...` | `hdms-backend` | 0 called by our code; 1 in an imported package and 1 in a required module, neither reachable | Clean / triaged |
| `gosec -exclude-generated -exclude=G115 ./...` | `hdms-backend` | 0 reported; 8 `#nosec` annotations with written justifications; 12 G115 hits suppressed rule-wide | Triaged & accepted |
| `pnpm audit --audit-level=high` | `hdms-frontend` | 0 | Clean |

The original scans (before the fixes) reported 4 govulncheck vulnerabilities in
`golang.org/x/crypto`, `google.golang.org/grpc` and `github.com/moby/go-archive`,
and 7 high-severity `pnpm audit` advisories in `fast-uri` and `js-yaml`. Both were
resolved by upgrading, not by suppression — see DEP-01 and DEP-02.

---

## 2. Security Findings & Triage Matrix

| ID | Finding / Rule | Component | Severity | Owner | Resolution / Acceptance | Review Date |
|---|---|---|---|---|---|---|
| **DEP-01** | `GO-2026-6355`, `GO-2026-6354`, `GO-2026-6348`, `GO-2026-6253` reported by `govulncheck` | `hdms-backend` dependencies | High | Santosh KC | **Resolved**: upgraded `golang.org/x/crypto` to v0.56.0, `google.golang.org/grpc` to v1.83.1 and `github.com/moby/go-archive` to v0.3.0. `govulncheck ./...` now reports 0 vulnerabilities affecting our code. | — |
| **DEP-02** | 7 high-severity `pnpm audit` advisories in `fast-uri` and `js-yaml` | `hdms-frontend` | High | Santosh KC | **Resolved**: `pnpm.overrides` pin `fast-uri >=3.1.6` and `js-yaml >=4.3.2`. `pnpm audit --audit-level=high` now reports no known vulnerabilities. | — |
| **DEP-03** | Two `govulncheck` notices in imported/required code that our code does not call | `hdms-backend` (transitive) | Low | Santosh KC | **Accepted**: not reachable from any HDMS call path — `govulncheck` classifies them as uncalled. Re-checked on every CI run; upgrade at the next dependency bump. | 2026-10-01 |
| **SEC-01** | `G124`: `HttpOnly: false` on the CSRF double-submit cookies | `internal/platform/auth/middleware.go`, `internal/platform/staffauth/cookies.go` (4 sites) | Low | Santosh KC | **Accepted**: required by the OWASP double-submit pattern — the SPA reads `hdms_csrf` / `hdms_staff_csrf` to echo it in `X-CSRF-Token`. Session cookies remain `HttpOnly`, `Secure`, `SameSite=Lax`. Each site carries an inline `#nosec G124` with this reason. | 2026-10-01 |
| **SEC-02** | `G703`: path traversal via taint analysis on operator-supplied CLI paths | `cmd/hdms-cli/main.go` (3 sites: `--file` import, `--out` export) | Low | Santosh KC | **Accepted**: local operator CLI flags, not reachable over HTTP; the operator already has the shell's file access. Paths are `filepath.Clean`ed and output modes tightened to 0600/0750. Each site carries an inline `#nosec G703`. | 2026-10-01 |
| **SEC-03** | `G101`: false positive on the `credential.revoked` event topic constant | `internal/platform/events/events.go` | Low | Santosh KC | **Resolved**: the literal is an event topic name, not a credential. Annotated `#nosec G101`; G101 is otherwise enforced. | — |
| **SEC-04** | `G115`: integer conversion overflow (12 hits) | `internal/platform/settings/service.go` (6), `internal/modules/identity/module.go` (4), `internal/platform/auth/password.go` (1), `internal/modules/credentials/module.go` (1) | Low | Santosh KC | **Accepted, rule-wide exclusion**: every hit converts a value a domain validator has already bounded (pagination limits, label rows/columns, session timeouts, hash lengths). This is the one rule excluded wholesale in CI, which means a future G115 anywhere in the backend will not be reported — revisit at the review date and replace with per-site `#nosec` if the count grows. | 2026-10-01 |

---

## 3. Container Base Image Digest Pinning

All base images across `docker-compose.yml`, `hdms-backend/Dockerfile.dev`, and `.github/workflows/ci.yml` are pinned by cryptographically verified SHA256 digests:

- **PostgreSQL 18 Alpine:**
  `postgres:18-alpine@sha256:d3e1620b530c944afa6e887d22eb899824da68e19c52024bf98f5220c88a65b2`
- **Caddy 2 Alpine:**
  `caddy:2-alpine@sha256:ad27e531c8b286ff153c0e6e16587a1583e4111bb58c4d83bd73d6d3ef0a0ce1`
- **Golang 1.26 Alpine:**
  `golang:1.26-alpine@sha256:ce864e7223ac17b1775e6fd0b4c0db580c2eb50e7953a427916379e4b92a1628`

---

## 4. Container Image Update Routine

To update base image digests safely during routine maintenance:

1. **Pull the latest version tag:**
   ```bash
   docker pull postgres:18-alpine
   docker pull caddy:2-alpine
   docker pull golang:1.26-alpine
   ```
2. **Inspect and resolve the new immutable repo digest:**
   ```bash
   docker buildx imagetools inspect <image-name>:<tag>   # use the multi-arch index digest
   ```
3. **Update references:**
   Update the pinned `@sha256:...` digest in:
   - `docker-compose.yml`
   - `hdms-backend/Dockerfile.dev`
   - `.github/workflows/ci.yml`
4. **Execute verification suite:**
   - Run integration tests: `go test -tags=integration ./test/...`
   - Run security scans: `govulncheck ./...`, `gosec ./...`
   - Run CI workflow validation
5. **Record in findings register:** Log the digest transition and verification date.
