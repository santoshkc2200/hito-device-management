# Threat-model walkthrough (Phase 5.2f)

**Date:** 2026-09-18 · **Owner:** Santosh KC
**Parent:** [5.2 Security Review](./5.2-security-review.md) · **Threat model:** [09](../../09-security-privacy-ops.md)
**Scope:** T1–T13 (including T2b and T11) walked item by item against the tree at `b2e12b2`.

Every row names the control and the test that proves it. Where the control is
operational rather than code, the row names the runbook and the pilot gate that
verifies it. Findings from this walk are THR-01–THR-06 in the
[findings register](./security-findings-register.md#5-threat-walkthrough-findings-52f); the automated-scan
findings (DEP/SEC) already live there from 5.2e.

---

## Walkthrough table

| # | Threat (09) | Control — where it lives | Test that proves it |
|---|---|---|---|
| T1 | Kiosk iPad stolen or removed | Kiosk bearer token scoped to 6 session ops + 2 probes (`internal/platform/auth/kioskscope.go`); no user create/list/modify (FR-45, INV-11); token revocable/disablable and rotatable from the console; session ids bound to the kiosk that opened them (`internal/apiserver/sessions.go:137`); Guided Access single-app lock + locked mount (`docs/runbooks/kiosk-ipad-setup.md` steps 6–7) | `TestKioskScopeCoversEntireSpec` (every spec op → 401/403), `TestEveryOperationHasAKioskScopeClassification` (unclassified op fails the build), `TestINV11_*` ×4, `TestHTTPKiosksLifecycle` + `settings_test.go` disable/rotate (disabled token rejected), `TestSessionIdentifierChangesOnLogin` |
| T2 | Borrowing on a colleague's card | **Deliberately accepted** (09): card possession is the factor, as with the paper register. Detection/response: every check failure writes an audit row; account can be suspended. | Acceptance THR-02. Supporting: `TestRoleDenialEmitsAuditEvent`, `TestInvariant_INV6_SuspendedArchivedCannotBorrow` |
| T2b | Unauthorised person obtains a borrowing account | Registration is administrator-only: no self-service path; kiosk token cannot reach any user-creating endpoint; `registered_by` rejects `kiosk:` actors at the module boundary | `TestINV11_KioskCannotCreateUser`, `TestINV11_RegisteredByNeverKiosk`, `TestKioskScopeCoversEntireSpec` (`POST /v1/users`, `POST /v1/users/register-with-card` → 403), `TestInvariant_INV11_KioskNeverCreatesUser` |
| T11 | Blank card stock stolen from the drawer | Unbound credentials resolve as `unbound` and are refused at scan; console shows unbound count so a shortfall is visible; drawer kept locked (operational) | `TestInvariant_INV12_UnboundNeverBorrows`, `TestCredentialsUnboundResolvesDistinctly`, `TestCredentialsCountUnbound`, sessions scan of unbound → `rejected` (`sessions_http_test.go:243`) |
| T3 | Found/stolen card used after loss | Reissue/revoke kills the old token (`status 'lost'`/`'revoked'`); dead cards resolve distinctly and every scan attempt is recorded in `scan_events` and surfaced on the dashboard | `TestInvariant_INV4_RevokedNeverBorrows`, `TestCredentialsReissueKillsOldKeepsHistory`, `TestCredentialsRevokedResolvesAsRevokedNotUnknown`, `TestCredentialsRevokeRequiresReason`, revoked-scan → `(revoked, rejected)` row (`checkout_scan_test.go:284`), dashboard revoked-scan alert (`dashboard.test.tsx`) |
| T4 | Fake barcode printed to impersonate a user | 50-bit random payload (10 × 5-bit Crockford Base32 symbols, `crypto/rand`), never derived from employee numbers; check character rejects misreads; subject hint is not authority (server resolves stored `subject_type`) | `TestGoldenFixture`, `TestGenerateRoundTrip`, `TestSingleCharacterMutationRejected`, `TestParseErrors`, `TestHintIsNotAuthoritative` (`internal/platform/tokens`), TS twin implementation checked against the same golden file |
| T5 | Database backup leaked | Tokens stored as `HMAC-SHA256(token, pepper)` with the pepper outside the DB (`internal/modules/credentials/internal/cryptox/cryptox.go`); device reprint uses AES-256-GCM under a separate key; CI fails on committed keys or a leaked example pepper | `cryptox_test.go` (HMAC determinism + pepper separation), CI `Scan for committed secrets` step. **Residual:** encrypted nightly backups do not exist yet — open item THR-03, owned by 5.4a |
| T6 | Admin account compromised | Argon2id passwords (12-char minimum), TOTP second factor + recovery codes, 5-attempt/15-min lockout, 12 h sliding sessions, `HttpOnly; Secure; SameSite=Lax` cookies, CSRF double-submit on every mutation, RBAC (admin/technician/viewer), session fixation purge on login and rotation on privilege change, append-only audit the app role cannot alter | `TestAccountLockoutAndUnlock`, `TestAdminPasswordResetAndForcedChange`, `TestAdminForceTotpReenrolment`, `TestRoleMatrixCoversEntireSpec`, `TestTechnicianRoleBoundaries`, `TestViewerRoleBlockedOnAllMutations`, `TestSessionIdentifierChangesOnLogin` (+ privilege-change rotation asserted in the same test), `TestStateChangingRequestWithoutCsrfIsRejected`, `TestAuditEventsAppendOnlyUnderAppRole`, `TestAuthAuditTrailNoPlaintextSecrets`. **Residual:** hospital-AD OIDC deferred to Phase 6 — accepted, THR-04 |
| T7 | Staff-location oracle | Error bodies carry `holderDepartment`, never the holder's name (`internal/apiserver/helpers.go:99`); names appear only in-session at the kiosk; a kiosk cannot open another kiosk's session (reported as not-found, `sessions.go:137`); tokens never reach URLs/query strings/referrers (`Referrer-Policy`, `WriteProblem` confines `instance` to path, span/audit sanitizers) | `TestTokenNeverAppearsInProblemDetails`, `TestTokenNeverAppearsInSpanAttributes`, `TestTokenNeverAppearsInAuditPayload`, `TestTokenNeverAppearsInLogOutput`, contract note `docs/06-api-contract.md:367` |
| T8 | SQL injection | sqlc-generated parameterised queries throughout; no string-built SQL in `internal/` (verified by grep in this review); `gosec` enforced in CI | CI `security` job (`gosec -exclude-generated -exclude=G115`, 0 reported), 5.2e register SEC-01–SEC-04 triage |
| T9 | Kiosk token exfiltrated from iPad storage | Scoped token (T1), rotation without reinstall, kiosk disable kills the token, per-kiosk session binding, kiosk-class rate limits bound the blast radius | `TestHTTPKiosksLifecycle`, rotate/disable tests (`settings_test.go:415`, `kiosks_http_test.go:45`), `TestKioskScopeCoversEntireSpec`, `TestLoginLockoutThresholdAndScanBurstAtReplayVolume` |
| T10 | Denial of service via stuck scanner trigger | Per-class rate limits (kiosk-scan 20 req/s + burst 300 sized for the 200-item 5.1c replay drain; login/pairing strict), `429` + `Retry-After` honoured by the kiosk client, server idempotency on mutations, client debounce | `TestLoginLockoutThresholdAndScanBurstAtReplayVolume`, `TestRateLimitKeyIsPerClientBehindTheProxy`, kiosk `api.test.ts` Retry-After handling, `idempotency_test.go`. **5.3a:** Caddy overwrites `X-Forwarded-For` with the peer IP (THR-01 closed for the spoofing vector; map eviction accepted as a single-host residual) |
| T12 | Admin backdates/fabricates a loan | Backfill is admin-only (kiosk → 403); every batch fully audited with actor, `recorded_at`, slip reference; `origin` immutable (INV-15); physical page retained and referenced by `paper_ref` (INV-14) | `TestBackfillRejectedForKioskToken`, `TestINV13_BackfillOverlapRejected`, `TestINV14_ProvenanceMandatory`, `TestINV15_OriginNeverMutated`, `TestInvariant_INV15_OriginNeverRewritten`, `TestHTTPBackfillHappyPathAndLastEntry` |
| T13 | Paper slips lost before being typed in | Structured pads + page numbers + "all rows entered" tick (operational); `GET /v1/backfill/last-entry` feeds a dashboard warning after the settings threshold (default 48 h); named daily task in the morning runbook | `TestHTTPBackfillHappyPathAndLastEntry` (last-entry), `TestHTTPDashboardEndpoint` (`paperBacklogHours: 48`), `settings_test.go:42` (default 48 h), dashboard `attention-paper-backlog` (`dashboard.test.tsx:373`). **Verification:** daily routine + 48 h typing proven in the pilot — THR-06 |

---

## Verification evidence (re-run 2026-09-18, this task)

| Proof | Command | Result |
|---|---|---|
| Kiosk token reaches no admin operation, per endpoint | `go test -tags=integration -run 'TestKioskScopeCoversEntireSpec\|TestEveryOperationHasAKioskScopeClassification\|TestINV11' ./test/integration/ -count=1` | PASS |
| Append-only grant rejects `UPDATE`/`DELETE` (SQLSTATE 42501), app-role startup check fails for owner / passes for `hdms_app` | `go test -tags=integration -run 'TestAuditEventsAppendOnlyUnderAppRole\|TestProductionStartupPrivilegeCheck' ./test/integration/ -count=1` | PASS |
| Redaction unit tests (logs, spans, problem bodies, audit payloads) | `go test ./internal/platform/httpx/... ./internal/platform/observability/... ./internal/modules/audit/...` | ok |
| Auth + token unit tests | `go test ./internal/platform/auth/... ./internal/platform/tokens/...` | ok |

---

## Review of the phase's changes (the `security-review` pass)

No `security-review` skill is installed in this environment, so the pass was
done manually over the 5.2a–e diff (`HEAD~3..HEAD`, 49 files): headers/CSP
boundary, tiered limiter, the four-path redaction, kiosk-scope matrix,
migration 0019 + startup privilege check, the login/staff fixation fix,
`#nosec` annotations, CLI path handling, and the CI security job. Notes:

- **Checked, no issue:** login/staff fixation revokes the presented session only
  after successful authentication (a failed attempt cannot log a live session
  out); privilege-change rotation is completed by the service layer, which
  revokes *all* sessions on an actual role change (`service.go:663`) — the
  existing test asserts the stale token → 401. CSRF `HttpOnly: false` is the
  double-submit pattern, session cookies stay `HttpOnly` (SEC-01, already
  accepted). Tracing `OnStart` redaction is effective — `TestTokenNeverAppearsInSpanAttributes`
  proves the exported spans carry no plaintext, so the SDK keeps the redacted
  value. The CI pepper scan's `docs/**` exclusion is justified: `docs/09` and
  the environment matrix legitimately quote the example value.
- **One finding:** THR-01 (XFF first-hop trust, three copies, no eviction) —
  registered, and since closed in 5.3a for the spoofing vector (Caddy
  `header_up` overwrite in both production and staging Caddyfiles; map
  eviction accepted as a single-host residual).
- No string-built SQL in `internal/`; `gosec`/`govulncheck`/`pnpm audit` status
  as recorded in the register §1–§2.
