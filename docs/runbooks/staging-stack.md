# Runbook: Staging Stack Operations & Rehearsal Target

This operational runbook covers the provisioning, verification, data seeding, and drill procedures for the **HDMS Staging Stack**.

---

## 1. Purpose & Rehearsal Mandate

**Rehearsal never happens against production.** Testing disaster recovery against a live system risks data loss; testing alerts in production produces operator fatigue; soak testing against the counter disrupts clinical staff.

The staging stack runs the **same Docker Compose definition as production** (`deploy/production/compose.yaml` plus the `docker-compose.staging.yml` override), with its own secrets and its own isolated database — the same production images (compiled API binary, Caddy with baked frontends), the same advisory-locked startup migrations, and production-like rate limiting.

Staging is the mandatory, designated operational target for:
1. **Restore Drills (Phase 5.4c):** Exercising full and point-in-time PostgreSQL database restores from automated backups.
2. **Alert & Fault Injection Tests (Phase 5.5c):** Deliberately terminating API instances, cutting database connectivity, and verifying Prometheus/Alertmanager notification routing to Hospital IT.
3. **Soak Testing (Phase 5.6b):** 24-hour sustained traffic soak test under realistic background load.
4. **Deployment & Rollback Rehearsals (Phase 5.3):** Rehearsing schema migrations and canary/blue-green image swaps before touching production.

---

## 2. Architecture & Isolation from Production

The staging environment is implemented via a Compose override (`docker-compose.staging.yml`) applied atop the production stack (`deploy/production/compose.yaml`, [5.3a](../phases/phase-5/5.3-deployment.md)), configured via `.env.staging`:

- **Project Namespace:** `hdms-staging` (creates isolated Docker network `hdms-staging_default` and volumes).
- **Database Service:** Runs `postgres:18-alpine` backed by volume `hdms-staging-db-data`.
- **Database Port:** Mapped to host port **`5443`** (development uses `5442`; default local Postgres uses `5432`; production is unmapped to the host).
- **Front Door:** Caddy serves staging on host port **`9443`** using `deploy/Caddyfile.staging` — the same shape as the production Caddyfile (baked `/` kiosk, `/admin` console, `/staff` PWA, `/v1/*` proxied with `X-Forwarded-For` overwritten) on a separate site address (development uses `8443`), so the staging and development stacks can be up at the same time — a drill must never require stopping dev.
- **API Service:** Runs the identical production image (compiled binary, no source bind-mount, no `air`) with only the localhost TLS material differing from production's hospital PKI mounts.
- **Rate Limiting:** Enabled (`HDMS_RATE_LIMIT=on`).
- **Secrets:** Dedicated staging keys (`HDMS_TOKEN_PEPPER`, `HDMS_CREDENTIAL_ENC_KEY`, `HDMS_TOTP_ENC_KEY`) generated independently of development and production.
- **UI Safeguard:** Frontends in staging display a prominent amber staging banner, and the borrower registration interface displays a warning banner prohibiting registration of real staff cards.

---

## 3. Mandatory Pre-Flight: Verifying Isolation from Production

Before executing any destructive rehearsal (database drop, restore drill, chaos injection), operators **must** verify that the staging stack is completely disconnected from production.

### Step 1: Inspect the Staging Database Connection String
Run:
```bash
docker compose -f deploy/production/compose.yaml -f docker-compose.staging.yml --env-file .env.staging exec api env | grep HDMS_DATABASE_URL
```

**Verification Criteria:**
1. **Target Host:** Must resolve to `db:5432` on the internal staging network or `localhost:5443` on the host. It must **never** contain a production IP address, hospital server hostname (`hdms-db.hospital.local`), or cloud RDS endpoint.
2. **Database Name:** Must be `hdms_staging`. It must **never** be `hdms`, `hdms_prod`, or `hdms_production`.
3. **Database User:** Must be `hdms_staging`.

### Step 2: Verify PostgreSQL Network and Database Identity
Connect to the staging database container and inspect database name and peer connections:
```bash
docker compose -f deploy/production/compose.yaml -f docker-compose.staging.yml --env-file .env.staging exec db psql -U hdms_staging -d hdms_staging -c "SELECT current_database(), inet_server_addr(), inet_server_port();"
```
Ensure `current_database()` reports `hdms_staging`.

### Step 3: Verify Isolated Volume Mounts
Check that the staging database volume is isolated:
```bash
docker volume ls --filter name=staging
```
Expected output: `hdms-staging_hdms-staging-db-data`.

---

## 4. Operational Commands (Taskfile)

Taskfile targets match the repository convention:

| Command | Action |
|---|---|
| `task staging:env` | Creates `.env.staging` from `.env.staging.example` if not present. |
| `task staging:up` | Builds and starts the staging stack (DB, API, Caddy) in the background. |
| `task staging:down` | Stops the staging stack containers and networks. |
| `task staging:seed` | Runs migrations and seeds the synthetic pilot-scale dataset (~800 staff, ~500 devices, 5 000 loans). |

### LAN test host on a Windows PC

To run staging on a Windows PC so testers on the same network can reach it, clone the repository, start Docker Desktop, and from an administrator PowerShell window at the repository root run:

```powershell
task staging:lan -- -Seed -AdminEmail admin@staging.test
# without Task installed:
powershell -ExecutionPolicy Bypass -File deploy\staging-lan.ps1 -Seed -AdminEmail admin@staging.test
```

`deploy/staging-lan.ps1` installs mkcert if missing, creates `.env.staging` with fresh secrets, issues a certificate covering the PC's LAN IP and hostname, opens TCP 9443 in Windows Firewall (Private/Domain networks), starts the stack, and prints the tester URLs. Testers must trust `certs\hdms-staging-rootCA.crt` once per device. Re-running it is safe; drop `-Seed` and `-AdminEmail` after the first run.

---

## 5. Pilot-Scale Synthetic Data Seeding

Staging must reflect hospital scale so performance and queries under drill conditions measure real characteristics.

### Running Seed Command
From the backend directory or via Taskfile:
```bash
# Via Taskfile
task staging:seed

# Or directly via CLI
HDMS_DATABASE_URL="postgres://hdms_staging:staging-db-password-change-me@localhost:5443/hdms_staging?sslmode=disable" go run ./cmd/hdms-cli seed --scale
```

### What is Populated:
- **Departments (8):** Emergency, Surgery, ICU, Cardiology, Pediatrics, Radiology, Neurology, Oncology.
- **Categories (7):** Infusion Pumps, Ultrasound Scanners, ECG Monitors, Defibrillators, Tablets, Laptops, Surgical Drills.
- **Users (800):** Synthetic staff members (`HH-SCALE-00001` .. `HH-SCALE-00800`) assigned across departments.
- **Devices (500):** Synthetic hospital equipment (`TAG-SCALE-00001` .. `TAG-SCALE-00500`) assigned to categories.
- **Historical Loans (5 000):**
  - 4 800 returned historical transactions spread across 2 000 hours.
  - 100 open on-time loans.
  - 100 open overdue loans.
- **Audit Log Events (~50 000):** Activity stream reflecting realistic operational volume.
- **Database Statistics:** Runs `ANALYZE` across all tables to build PostgreSQL query planner statistics.
