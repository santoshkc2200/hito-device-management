# Runbook — Production database roles

**Applies to:** production and staging deployments of HDMS
**Related:** [5.2d](../phases/phase-5/5.2-security-review.md), INV-8 (append-only audit),
[environment matrix](../phases/phase-5/environment-matrix.md)

## Why two roles

Migration `0019_audit_append_only.sql` splits the database identities in two:

| Role | Used by | Privileges on `audit_events` |
|---|---|---|
| Database owner (e.g. `hdms_prod`) | `hdms-cli migrate`, backups, DBA work | Full — it owns the table |
| `hdms_app` | The API process at runtime | `INSERT`, `SELECT` only. `UPDATE`, `DELETE` and `TRUNCATE` are revoked |

The audit trail is append-only because the runtime role *cannot* rewrite it, not
because the code chooses not to. If the application connects as the owner, the
grant proves nothing — so `hdms-api` refuses to start when `HDMS_ENV=production`
and the connected role still holds `UPDATE` on `audit_events`.

## One-time setup on a new production database

For Docker installs, no manual setup is required: the `worker` container automatically provisions the `hdms_app` login and applies the password from `HDMS_APP_DB_PASSWORD` at every start before launching the API.

For non-Docker installs, the migration creates `hdms_app` as `NOLOGIN`: a migration must not carry a
password. Grant it login rights once, with a password taken from the hospital's
secret store:

```sql
ALTER ROLE hdms_app WITH LOGIN PASSWORD '<from the vault>';
```

Then point the application's `HDMS_DATABASE_URL` at that role:

```
postgres://hdms_app:<password>@<prod-db>:5432/hdms_prod?sslmode=disable
```

Migrations keep using the owner's connection string, which is held separately and
is not given to the running application.

### Verify

```sql
-- as hdms_app: must return false
SELECT has_table_privilege('hdms_app', 'audit_events', 'UPDATE');

-- as hdms_app: must fail with SQLSTATE 42501
UPDATE audit_events SET payload = '{}' WHERE id = (SELECT id FROM audit_events LIMIT 1);
```

Starting `hdms-api` with `HDMS_ENV=production` is itself a check: it exits with
`production connection role ... has UPDATE privilege on audit_events` if the DSN
still points at the owner.

## Rotating the hdms_app password

On Docker installs:
1. In `/etc/hdms/hdms.env`, update the password in both `HDMS_APP_DB_PASSWORD` and `HDMS_DATABASE_URL`.
2. Apply the change:
   ```bash
   sudo docker compose -f deploy/production/compose.yaml --env-file /etc/hdms/hdms.env up -d
   ```
   The `worker` container starts first, applies the new password to `hdms_app` via `ALTER ROLE`, and the `api` container reconnects with the new credentials.

On non-Docker installs:
1. `ALTER ROLE hdms_app WITH PASSWORD '<new password from the vault>';`
2. Update `HDMS_DATABASE_URL` in the secret store.
3. Restart the API. The startup privilege check runs again on the new connection.

## After adding a new table

`0019` grants DML on all tables that existed when it ran, and sets default
privileges so tables created later by the same owner are granted automatically.
A table created by a *different* owner needs an explicit grant:

```sql
GRANT SELECT, INSERT, UPDATE, DELETE ON <new_table> TO hdms_app;
```

A new table that must also be append-only needs the same `REVOKE` treatment
`audit_events` gets in `0019`.
