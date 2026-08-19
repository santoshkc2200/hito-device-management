-- +goose Up
-- +goose StatementBegin

CREATE TABLE departments (
    id          uuid PRIMARY KEY,
    name        text NOT NULL UNIQUE,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TYPE user_status AS ENUM ('active', 'suspended', 'archived');

CREATE TABLE users (
    id             uuid PRIMARY KEY,
    employee_no    text NOT NULL,
    full_name      text NOT NULL,
    department_id  uuid REFERENCES departments(id),
    email          text,
    phone          text,
    status         user_status NOT NULL DEFAULT 'active',
    notes          text,
    registered_at  timestamptz NOT NULL DEFAULT now(),
    registered_by  text NOT NULL,          -- 'admin:<id>' | 'import'  (INV-11: never 'kiosk:*')
    updated_at     timestamptz NOT NULL DEFAULT now()
);

-- Employee numbers are unique among live users; archived duplicates are
-- allowed so a re-hire does not collide with the historical record
-- (INV-10: archive, never delete, a user with loan history).
CREATE UNIQUE INDEX users_employee_no_live_uk
    ON users (lower(employee_no)) WHERE status <> 'archived';

CREATE INDEX users_department_idx ON users (department_id);
CREATE INDEX users_status_idx     ON users (status);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE users;
DROP TYPE user_status;
DROP TABLE departments;

-- +goose StatementEnd
