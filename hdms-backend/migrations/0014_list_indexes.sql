-- +goose Up
-- +goose StatementBegin

-- 4.2a: Covering indexes for list surfaces, each ending in id for keyset pagination stability.

-- loans:
CREATE INDEX IF NOT EXISTS loans_status_due_at_id_idx ON loans (status, due_at, id);
CREATE INDEX IF NOT EXISTS loans_user_id_borrowed_at_id_idx ON loans (user_id, borrowed_at DESC, id DESC);

-- devices:
CREATE INDEX IF NOT EXISTS devices_status_asset_tag_id_idx ON devices (status, asset_tag, id);

-- users:
CREATE INDEX IF NOT EXISTS users_department_id_full_name_id_idx ON users (department_id, full_name, id);

-- audit:
CREATE INDEX IF NOT EXISTS audit_events_at_id_idx ON audit_events (at DESC, id DESC);
CREATE INDEX IF NOT EXISTS audit_events_actor_at_id_idx ON audit_events (actor, at DESC, id DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS audit_events_actor_at_id_idx;
DROP INDEX IF EXISTS audit_events_at_id_idx;
DROP INDEX IF EXISTS users_department_id_full_name_id_idx;
DROP INDEX IF EXISTS devices_status_asset_tag_id_idx;
DROP INDEX IF EXISTS loans_user_id_borrowed_at_id_idx;
DROP INDEX IF EXISTS loans_status_due_at_id_idx;

-- +goose StatementEnd
