# Environment Matrix and Configuration Specification

This document details every `HDMS_*` configuration variable used across the Hito Device Management System (HDMS) environments: **Development**, **Staging**, and **Production**.

Derived directly from the codebase (`internal/platform/config/config.go`, `internal/platform/httpx/middleware.go`, `.env.example`, `docker-compose.yml`, and `.github/workflows/ci.yml`).

## Configuration Matrix

| Variable | Secret? | Development (`task dev` / local) | Staging (`task staging:up`) | Production (`HDMS_ENV=production`) | Production Source / Provisioning Mechanism | Notes & Behavior |
|---|---|---|---|---|---|---|
| `HDMS_ENV` | No | `development` (or unset) | `staging` | `production` | Container / Host environment (`compose.yaml` or systemd) | Activates fail-closed safety validations when set to `production`. |
| `HDMS_HTTP_ADDR` | No | `:8443` | `:8443` | `:8443` | Container environment | Internal listener port. Terminated by Caddy on host port 8443. |
| `HDMS_DATABASE_URL` | **Yes** | `postgres://hdms:hdms@localhost:5442/hdms?sslmode=disable` | `postgres://hdms_staging:<staging_pw>@db:5432/hdms_staging?sslmode=disable` | `postgres://hdms_app:<prod_pw>@<prod-db>:5432/hdms_prod?sslmode=verify-full` | Hospital IT Vault / KMS (injected as env secret) | Connection string containing DB credentials. Redacted in startup logs (`xxxxx`). In production, the application connects as `hdms_app` (restricted role with append-only privileges on `audit_events`, INV-8); migrations are executed as the database owner. Startup checks enforce that the runtime role does not hold `UPDATE` privileges on `audit_events`. |
| `HDMS_TLS_CERT_FILE` | No | `certs/localhost.pem` | `certs/localhost.pem` (or staging CA cert) | `/etc/ssl/certs/hdms.hospital.crt` (path to CA-issued cert) | Hospital Internal CA / IT PKI infrastructure | Refuses to start in production if unset, points to `localhost.pem`, or contains `localhost`. |
| `HDMS_TLS_KEY_FILE` | **Yes** | `certs/localhost-key.pem` | `certs/localhost-key.pem` (or staging CA key) | `/etc/ssl/private/hdms.hospital.key` | Hospital Internal CA / IT PKI (file permissions 0600 root/hdms) | Refuses to start in production if unset, points to `localhost-key.pem`, or contains `localhost`. |
| `HDMS_TLS_ENABLED` | No | Unset (enabled) | Unset (enabled) | Unset or `true` | Container environment | Refuses to start in production if explicitly set to `false` or `off`. |
| `HDMS_TOKEN_PEPPER` | **Yes** | `dev-only-pepper-change-me` | Dedicated random 32-byte hex string | Cryptographically random 32-byte hex string (`openssl rand -hex 32`) | Hospital IT Vault / 1Password (backed up independently from DB) | HMAC pepper for credential tokens. Production refuses to boot if equal to dev default `dev-only-pepper-change-me`. |
| `HDMS_CREDENTIAL_ENC_KEY` | **Yes** | Dev base64 key in `.env.example` | Dedicated random 32-byte base64 key | Cryptographically random 32-byte base64 key (`openssl rand -base64 32`) | Hospital IT Vault / KMS (backed up independently from DB) | AES-256-GCM symmetric key for reversible credential token encryption (`token_enc`). Mandatory in production. *(See Plan Discrepancy Note below)* |
| `HDMS_TOTP_ENC_KEY` | **Yes** | Dev base64 key in `.env.example` | Dedicated random 32-byte base64 key | Cryptographically random 32-byte base64 key (`openssl rand -base64 32`) | Hospital IT Vault / KMS (backed up independently from DB) | AES-256-GCM symmetric key for admin TOTP secrets at rest (`admin_accounts.totp_secret_enc`). Mandatory in production. *(See Plan Discrepancy Note below)* |
| `HDMS_SESSION_TTL` | No | `25s` (default) | `25s` | `25s` | Container environment | Idle kiosk scan session timeout before auto-cancelling in-progress borrow/return sessions. |
| `HDMS_ADMIN_SESSION_TTL` | No | `12h` (default) | `12h` | `12h` | Container environment | Admin login session sliding renewal timeout. |
| `HDMS_STAFF_SESSION_TTL` | No | `12h` (default) | `12h` | `12h` | Container environment | Staff portal login session timeout. |
| `HDMS_RATE_LIMIT` | No | `off` in dev compose, otherwise default | `on` | `on` (or unset, defaulting to enabled) | Container environment | Refuses to start in production if set to `off`. Evaluated directly into `config.Config.RateLimitEnabled`. |
| `HDMS_CORS_ALLOWED_ORIGINS` | No | Defaults to `https://localhost:5173,https://localhost:5174` | Unset (same-origin) or staging origins | Unset (same-origin behind Caddy) or specific hospital origins | Container environment | In production, refuses to boot if set to `*` or containing `localhost` / `127.0.0.1` / `http://`. Evaluated into `config.Config.CORSAllowedOrigins`. |
| `HDMS_ENTRA_TENANT_ID` | No | Unset (disabled) | Dedicated staging tenant ID (or unset) | Production Azure AD / Entra Tenant UUID | Hospital Azure Active Directory (Entra ID) | Optional: if unset, Entra SSO is disabled and local staff auth is used. |
| `HDMS_ENTRA_CLIENT_ID` | No | Unset (disabled) | Dedicated staging client ID (or unset) | Production App Registration Client ID | Hospital Azure Active Directory (Entra ID) | Optional: Azure AD application client ID. |
| `HDMS_ENTRA_CLIENT_SECRET` | **Yes** | Unset (disabled) | Dedicated staging client secret (or unset) | Production App Registration Secret | Hospital Azure Active Directory / Vault | Redacted in startup logs (`[REDACTED]`). |
| `HDMS_ENTRA_REDIRECT_URL` | No | `https://localhost:8443/v1/staff/auth/microsoft/callback` | Staging callback URL | `https://hdms.hospital.local/v1/staff/auth/microsoft/callback` | Azure AD App Registration Redirect URI | OIDC redirect URI for Entra SSO flow. |
| `HDMS_ENTRA_ALLOWED_EMAIL_DOMAINS` | No | Unset | Staging email domains | `hospital.org,hito-hospital.jp` | Hospital HR / IT policy | Comma-separated list of allowed email domains for Entra staff login. |
| `HDMS_OTLP_ENDPOINT` | No | Unset (exports to stdout) | Staging collector endpoint | Production OpenTelemetry Collector URL (e.g. `http://otel-collector:4318`) | Hospital Monitoring Infrastructure | Empty string exports spans to stdout. |
| `HDMS_LOG_LEVEL` | No | `info` (or `debug`) | `info` | `info` (or `warn`) | Container environment | Log level for structured slog logger (`debug`, `info`, `warn`, `error`). |
| `HDMS_TEST_ADMIN_EMAIL` | No | `admin@example.org` | Unset | Unset | N/A (Dev only) | Used only for dev / Playwright test account bootstrap. Never set in production. |
| `HDMS_TEST_ADMIN_PASSWORD` | **Yes** | `correct horse battery staple` | Unset | Unset | N/A (Dev only) | Used only for dev / Playwright test account bootstrap. Never set in production. |
| `HDMS_TEST_ADMIN_TOTP_SECRET` | **Yes** | `VIPF7BGMNRBOPVSVYOIAG33Q5NHWVOZ7` | Unset | Unset | N/A (Dev only) | Used only for dev / Playwright test account bootstrap. Never set in production. |
| `HDMS_API_URL` | No | `https://localhost:8443` | `https://localhost:8443` | Unset | CI / Test Runner | Used by Playwright E2E test runner to target API backend. |

---

## Plan Discrepancies and Architectural Notes

### 1. Key Naming: `DEVICE_TOKEN_KEY` and `SESSION_KEY`
- In `docs/phases/phase-5/5.0-preflight.md`, the spec notes `DEVICE_TOKEN_KEY` and `SESSION_KEY`.
- In the real codebase, the persistent encryption keys were formalized in Phase 7 and ADR-0016 as:
  - **`HDMS_CREDENTIAL_ENC_KEY`**: AES-256-GCM 32-byte key for reversible device credential and user badge token storage (`token_enc`).
  - **`HDMS_TOTP_ENC_KEY`**: AES-256-GCM 32-byte key for admin TOTP secrets stored at rest (`admin_accounts.totp_secret_enc`).
  - **`HDMS_TOKEN_PEPPER`**: HMAC-SHA256 pepper for credential hash verification (`token_hash`).
- Production fail-closed gates enforce `HDMS_CREDENTIAL_ENC_KEY`, `HDMS_TOTP_ENC_KEY`, and `HDMS_TOKEN_PEPPER`.

### 2. Rate Limiting and CORS Centralization
- Previously, rate limiting and CORS allowlists checked environment variables (`os.Getenv("HDMS_RATE_LIMIT")`, `os.Getenv("HDMS_ENV")`) or hardcoded arrays inside `cmd/hdms-api/main.go` and `internal/platform/httpx/middleware.go`.
- Under 5.0b, these decisions are loaded and validated in `internal/platform/config/config.go` (`RateLimitEnabled` and `CORSAllowedOrigins`) and passed cleanly into HTTP middleware, preventing any production configuration bypass.

### 3. Fail-Closed Production Refusals
When `HDMS_ENV=production`, startup immediately halts if:
1. `HDMS_TOKEN_PEPPER` equals `dev-only-pepper-change-me` (from `.env.example`).
2. `HDMS_RATE_LIMIT` is set to `"off"`.
3. `HDMS_TLS_ENABLED` is set to `"false"` or `"off"`, or `HDMS_TLS_CERT_FILE` / `HDMS_TLS_KEY_FILE` are empty or reference dev localhost certificates (`certs/localhost.pem`, `certs/localhost-key.pem`).
4. `HDMS_CORS_ALLOWED_ORIGINS` contains `*` (wildcard) or unencrypted/localhost domains (`localhost`, `127.0.0.1`, `http://`).
5. `HDMS_CREDENTIAL_ENC_KEY` or `HDMS_TOTP_ENC_KEY` are unset or not 32 bytes.
6. The connected database role holds `UPDATE` privilege on `audit_events` (enforced via startup query `has_table_privilege(current_user, 'audit_events', 'UPDATE')` — production application runtime must connect as `hdms_app` to preserve the append-only audit trail, INV-8).

Granting `hdms_app` login rights and pointing the application at it is a one-time
operator step — see [the production database roles runbook](../../runbooks/production-database-roles.md).
