-- +goose Up
-- +goose StatementBegin

-- 6.2b / 6.2d: Notification delivery pipeline, preferences, and overdue escalation tracking.
--
-- delivery_log records every reminder or digest attempt. Deduplication on
-- (loan, escalation step) is enforced by the unique dedupe_key column before
-- any delivery attempt is made (INV-dedupe). Quarantined deliveries (exhausted retries)
-- remain visible to administrators.
CREATE TABLE delivery_log (
    id               uuid PRIMARY KEY,
    recipient        text NOT NULL,
    channel          text NOT NULL DEFAULT 'email',
    template         text NOT NULL,
    dedupe_key       text NOT NULL UNIQUE,
    attempt_count    int NOT NULL DEFAULT 0,
    status           text NOT NULL CHECK (status IN ('pending', 'queued_quiet_hours', 'sent', 'quarantined', 'suppressed', 'failed')),
    last_error       text,
    next_attempt_at  timestamptz NOT NULL DEFAULT now(),
    loan_id          uuid REFERENCES loans(id) ON DELETE SET NULL,
    user_id          uuid REFERENCES users(id) ON DELETE SET NULL,
    escalation_step  int,
    payload          jsonb NOT NULL DEFAULT '{}',
    created_at       timestamptz NOT NULL DEFAULT now(),
    sent_at          timestamptz,
    updated_at       timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX delivery_log_status_idx ON delivery_log (status, next_attempt_at);
CREATE INDEX delivery_log_loan_idx ON delivery_log (loan_id);
CREATE INDEX delivery_log_recipient_idx ON delivery_log (recipient);
CREATE INDEX delivery_log_quarantined_idx ON delivery_log (status, created_at DESC) WHERE status = 'quarantined';

-- 6.2d: Per-user channel preferences and opt-out, managed by administrators.
CREATE TABLE notification_preferences (
    user_id     uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    channel     text NOT NULL DEFAULT 'email',
    opted_out   boolean NOT NULL DEFAULT false,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    updated_by  text NOT NULL DEFAULT 'system'
);

-- 6.2a: Overdue escalation tracker for overdue-scan job.
-- Ensures a loan publishes at most once per escalation step, even when
-- scans run hourly.
CREATE TABLE overdue_escalations (
    loan_id          uuid NOT NULL REFERENCES loans(id) ON DELETE CASCADE,
    escalation_step  int NOT NULL,
    published_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (loan_id, escalation_step)
);

CREATE INDEX overdue_escalations_loan_idx ON overdue_escalations (loan_id);

-- Explicit grants for the application runtime user
GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE delivery_log TO hdms_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE notification_preferences TO hdms_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE overdue_escalations TO hdms_app;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS overdue_escalations;
DROP TABLE IF EXISTS notification_preferences;
DROP TABLE IF EXISTS delivery_log;

-- +goose StatementEnd
