-- +goose Up
-- +goose StatementBegin

-- 5.2d: Dedicated application runtime role with append-only privileges on
-- audit_events (INV-8). In production, the backend connects as hdms_app,
-- while migrations are executed as the database owner.
--
-- The role is created NOLOGIN on purpose: a migration must not carry a
-- password. Before the application can connect as hdms_app, the operator
-- grants it login rights once, out of band, with the password held in the
-- hospital's secret store:
--
--   ALTER ROLE hdms_app WITH LOGIN PASSWORD '<from the vault>';
--
-- See docs/runbooks/production-database-roles.md.
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hdms_app') THEN
        CREATE ROLE hdms_app NOLOGIN;
    END IF;
END
$$;

-- Schema usage
GRANT USAGE ON SCHEMA public TO hdms_app;

-- Full DML on all existing tables
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO hdms_app;

-- Revoke UPDATE, DELETE, and TRUNCATE on audit_events so audit rows cannot be modified or deleted
REVOKE ALL ON TABLE audit_events FROM PUBLIC;
REVOKE UPDATE, DELETE, TRUNCATE ON TABLE audit_events FROM hdms_app;
GRANT SELECT, INSERT ON TABLE audit_events TO hdms_app;

-- Grant sequence usage for auto-incrementing columns (e.g. outbox_id_seq)
GRANT USAGE, SELECT, UPDATE ON ALL SEQUENCES IN SCHEMA public TO hdms_app;

-- Ensure future tables and sequences created by migrations also grant DML/usage to hdms_app
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO hdms_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT, UPDATE ON SEQUENCES TO hdms_app;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE ALL ON TABLES FROM hdms_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE ALL ON SEQUENCES FROM hdms_app;
REVOKE ALL ON ALL TABLES IN SCHEMA public FROM hdms_app;
REVOKE ALL ON ALL SEQUENCES IN SCHEMA public FROM hdms_app;
REVOKE ALL ON SCHEMA public FROM hdms_app;
DROP OWNED BY hdms_app;

DO $$
BEGIN
    BEGIN
        DROP ROLE IF EXISTS hdms_app;
    EXCEPTION WHEN dependent_objects_still_exist THEN
        -- Role may be referenced in other template/clone databases in test clusters;
        -- privileges in this database are already completely revoked.
        NULL;
    END;
END
$$;

-- +goose StatementEnd
