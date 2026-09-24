-- +goose Up
-- +goose StatementBegin

-- Remote copies of the backup repository. The local host repository is NOT a
-- row here: it is HDMS_BACKUP_DIR from the root-owned environment file, always
-- written, with fixed 30-daily + 12-monthly retention.
CREATE TABLE backup_destinations (
    id                 uuid PRIMARY KEY,
    name               text NOT NULL,
    kind               text NOT NULL CHECK (kind IN ('path', 'rclone')),
    target             text NOT NULL,
    provider           text NOT NULL CHECK (provider IN ('lan', 'google_drive', 'onedrive')),
    enabled            boolean NOT NULL DEFAULT true,
    retention_versions integer NOT NULL DEFAULT 2 CHECK (retention_versions >= 1),
    initialized_at     timestamptz,
    last_ok_at         timestamptz,
    last_error         text,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    updated_by         text NOT NULL
);

-- Two rows pointing at one repository would make retention non-deterministic:
-- both would prune the same repository in the same run with different limits.
CREATE UNIQUE INDEX backup_destinations_target_key ON backup_destinations (kind, target);

GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE backup_destinations TO hdms_app;

-- A fan-out run where the local snapshot succeeded but a destination failed is
-- neither a success nor a failure: the database is protected, one copy is not.
-- 0020 predates that third outcome, so the check has to admit it.
ALTER TABLE job_runs DROP CONSTRAINT job_runs_outcome_check;
ALTER TABLE job_runs ADD CONSTRAINT job_runs_outcome_check
    CHECK (outcome IN ('success', 'degraded', 'failure'));

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Degraded rows must go before the stricter check can come back.
DELETE FROM job_runs WHERE outcome = 'degraded';
ALTER TABLE job_runs DROP CONSTRAINT job_runs_outcome_check;
ALTER TABLE job_runs ADD CONSTRAINT job_runs_outcome_check
    CHECK (outcome IN ('success', 'failure'));

DROP TABLE IF EXISTS backup_destinations;

-- +goose StatementEnd
