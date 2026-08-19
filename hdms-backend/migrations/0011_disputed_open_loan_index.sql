-- +goose Up
-- +goose StatementBegin

-- 2.4b: a disputed loan is a recorded claim, never a custody fact, so it must
-- not block a real one. The exclusion constraint already excludes disputed
-- rows (0007 decided that), but loans_one_open_per_device_uk did not — an
-- open disputed row would sit in the unique index and make every future
-- borrow of that device fail with 23505, exactly the opposite of what
-- "recorded outside the constraint" promised. Rebuild the index with the
-- same predicate the exclusion constraint uses for "counts as custody".
DROP INDEX loans_one_open_per_device_uk;

CREATE UNIQUE INDEX loans_one_open_per_device_uk
    ON loans (device_id) WHERE status = 'open' AND NOT disputed;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX loans_one_open_per_device_uk;

CREATE UNIQUE INDEX loans_one_open_per_device_uk
    ON loans (device_id) WHERE status = 'open';

-- +goose StatementEnd
