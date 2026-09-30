-- +goose Up
-- +goose StatementBegin

-- Backup schedule, edited from the admin console and read by the worker on
-- every tick. One row; the defaults reproduce the old 02:00 timer.
CREATE TABLE backup_schedule (
    id               smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    enabled          boolean NOT NULL DEFAULT true,
    mode             text NOT NULL DEFAULT 'daily' CHECK (mode IN ('interval', 'daily', 'weekly')),
    interval_minutes integer NOT NULL DEFAULT 360 CHECK (interval_minutes BETWEEN 15 AND 720),
    time_local       text NOT NULL DEFAULT '02:00' CHECK (time_local ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$'),
    weekday          smallint NOT NULL DEFAULT 0 CHECK (weekday BETWEEN 0 AND 6),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    updated_by       text NOT NULL DEFAULT 'system'
);
INSERT INTO backup_schedule (id) VALUES (1);

-- Work the console asks the worker to do. The API only inserts; the worker
-- claims with FOR UPDATE SKIP LOCKED and records the outcome.
CREATE TABLE backup_requests (
    id             uuid PRIMARY KEY,
    kind           text NOT NULL CHECK (kind IN ('run', 'test', 'verify')),
    destination_id uuid REFERENCES backup_destinations (id) ON DELETE CASCADE,
    requested_by   text NOT NULL,
    requested_at   timestamptz NOT NULL DEFAULT now(),
    started_at     timestamptz,
    finished_at    timestamptz,
    outcome        text CHECK (outcome IN ('success', 'degraded', 'failure')),
    detail         jsonb NOT NULL DEFAULT '{}'
);
CREATE INDEX backup_requests_pending ON backup_requests (requested_at) WHERE started_at IS NULL;
-- At most one pending "run": a second Back up now returns the first.
CREATE UNIQUE INDEX backup_requests_one_pending_run ON backup_requests (kind)
    WHERE started_at IS NULL AND kind = 'run';

-- The worker's copy of each repository's snapshot list, so the console never
-- waits on restic. repo_key is 'local' or a destination id as text.
CREATE TABLE backup_snapshots (
    repo_key     text NOT NULL,
    snapshot_id  text NOT NULL,
    taken_at     timestamptz NOT NULL,
    size_bytes   bigint NOT NULL DEFAULT 0,
    verified_at  timestamptz,
    refreshed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repo_key, snapshot_id)
);

-- Process-level state shared by the worker and the API. One row.
CREATE TABLE system_state (
    id             smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    worker_seen_at timestamptz
);
INSERT INTO system_state (id) VALUES (1);

GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE
    backup_schedule, backup_requests, backup_snapshots, system_state TO hdms_app;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE system_state;
DROP TABLE backup_snapshots;
DROP TABLE backup_requests;
DROP TABLE backup_schedule;
-- +goose StatementEnd
