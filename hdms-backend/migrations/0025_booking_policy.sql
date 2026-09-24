-- +goose Up
ALTER TABLE settings
    ADD COLUMN booking_advance_days int NOT NULL DEFAULT 90 CHECK (booking_advance_days BETWEEN 1 AND 365),
    ADD COLUMN booking_max_duration_days int NOT NULL DEFAULT 30 CHECK (booking_max_duration_days BETWEEN 1 AND 365),
    ADD COLUMN booking_return_buffer_minutes int NOT NULL DEFAULT 60 CHECK (booking_return_buffer_minutes BETWEEN 0 AND 1440);

-- +goose Down
ALTER TABLE settings
    DROP COLUMN booking_return_buffer_minutes,
    DROP COLUMN booking_max_duration_days,
    DROP COLUMN booking_advance_days;
