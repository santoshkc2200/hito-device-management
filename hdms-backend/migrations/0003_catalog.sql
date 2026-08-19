-- +goose Up
-- +goose StatementBegin

CREATE TABLE device_categories (
    id                  uuid PRIMARY KEY,
    name                text NOT NULL UNIQUE,
    default_loan_period interval,           -- NULL = no due date
    requires_approval   boolean NOT NULL DEFAULT false,
    created_at          timestamptz NOT NULL DEFAULT now()
);

CREATE TYPE device_status AS ENUM
    ('available', 'on_loan', 'maintenance', 'retired', 'lost');

CREATE TYPE device_condition AS ENUM ('good', 'fair', 'damaged');

CREATE TABLE devices (
    id             uuid PRIMARY KEY,
    asset_tag      text NOT NULL,
    name           text NOT NULL,
    category_id    uuid NOT NULL REFERENCES device_categories(id),
    manufacturer   text,
    model          text,
    serial_no      text,
    status         device_status NOT NULL DEFAULT 'available',
    condition      device_condition NOT NULL DEFAULT 'good',
    home_location  text,
    notes          text,
    acquired_on    date,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);

-- devices.status is a denormalised projection of loan state (Phase 2
-- maintains it in the same transaction as the loan); NOT NULL / DEFAULT
-- above only cover this module's own transitions until then.
CREATE UNIQUE INDEX devices_asset_tag_live_uk
    ON devices (upper(asset_tag)) WHERE status <> 'retired';
CREATE INDEX devices_status_idx   ON devices (status);
CREATE INDEX devices_category_idx ON devices (category_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE devices;
DROP TYPE device_condition;
DROP TYPE device_status;
DROP TABLE device_categories;

-- +goose StatementEnd
