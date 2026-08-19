-- +goose Up
-- +goose StatementBegin

-- platform: outbox retry accounting. A handler that fails permanently used
-- to be retried forever with no record of it, and because a batch is
-- claimed `ORDER BY id LIMIT 100`, a hundred such rows at the head of the
-- queue would starve every newer event behind them — the whole outbox
-- stuck on one poison message.
--
-- attempts/next_attempt_at back off a failing row instead of hammering it
-- every 500ms, and failed_at retires it from the queue entirely once it has
-- exhausted its attempts, so the head of the queue can always move. A
-- dead-lettered row is never deleted: last_error is the record of what went
-- wrong, and re-dispatching it is a deliberate act (clear failed_at).
ALTER TABLE outbox
    ADD COLUMN attempts        integer NOT NULL DEFAULT 0,
    ADD COLUMN next_attempt_at timestamptz NOT NULL DEFAULT now(),
    ADD COLUMN last_error      text,
    ADD COLUMN failed_at       timestamptz;

-- Replaces outbox_unpublished_idx: the claim query now also filters on
-- failed_at and next_attempt_at, and orders by id within what is due.
DROP INDEX outbox_unpublished_idx;
CREATE INDEX outbox_dispatchable_idx ON outbox (next_attempt_at, id)
    WHERE published_at IS NULL AND failed_at IS NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX outbox_dispatchable_idx;
CREATE INDEX outbox_unpublished_idx ON outbox (id) WHERE published_at IS NULL;

ALTER TABLE outbox
    DROP COLUMN attempts,
    DROP COLUMN next_attempt_at,
    DROP COLUMN last_error,
    DROP COLUMN failed_at;

-- +goose StatementEnd
