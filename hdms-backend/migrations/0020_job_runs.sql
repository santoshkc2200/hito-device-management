-- +goose Up
-- +goose StatementBegin

-- 5.4a: shared job_runs table for every scheduled job (backup, reconcile,
-- retention). Each run reports success positively: the alert fires on the
-- *absence* of a recent success row, never on "no error was seen".
-- Backward compatible: brand-new table, no existing readers/writers, safe
-- under mixed-version rollout during the pilot window.
CREATE TABLE job_runs (
    id          bigserial PRIMARY KEY,
    job         text NOT NULL,
    started_at  timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz,
    outcome     text NOT NULL CHECK (outcome IN ('success', 'failure')),
    detail      jsonb NOT NULL DEFAULT '{}'
);
CREATE INDEX job_runs_job_started_idx ON job_runs (job, started_at DESC);

-- Explicit grant: 0019's default privileges cover future tables created by
-- the owner, but the backup CLI must work even after Down/Up cycles in
-- test clusters, so grant directly.
GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE job_runs TO hdms_app;
GRANT USAGE, SELECT, UPDATE ON SEQUENCE job_runs_id_seq TO hdms_app;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS job_runs;

-- +goose StatementEnd
