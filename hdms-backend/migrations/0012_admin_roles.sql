-- +goose Up
-- +goose StatementBegin

-- 4.1a: Migrate admin_role enum to match docs/08-admin-console.md specification.
-- Old values ('superadmin', 'admin', 'operator') map to ('admin', 'admin', 'technician').
-- The new role 'viewer' is introduced, and becomes the least-privilege column default.

CREATE TYPE admin_role_new AS ENUM ('admin', 'technician', 'viewer');

ALTER TABLE admin_accounts
    ALTER COLUMN role DROP DEFAULT,
    ALTER COLUMN role TYPE admin_role_new USING (
        CASE role::text
            WHEN 'superadmin' THEN 'admin'::admin_role_new
            WHEN 'admin'      THEN 'admin'::admin_role_new
            WHEN 'operator'   THEN 'technician'::admin_role_new
            ELSE 'viewer'::admin_role_new
        END
    ),
    ALTER COLUMN role SET DEFAULT 'viewer'::admin_role_new;

DROP TYPE admin_role;
ALTER TYPE admin_role_new RENAME TO admin_role;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

CREATE TYPE admin_role_old AS ENUM ('superadmin', 'admin', 'operator');

ALTER TABLE admin_accounts
    ALTER COLUMN role DROP DEFAULT,
    ALTER COLUMN role TYPE admin_role_old USING (
        CASE role::text
            WHEN 'admin'      THEN 'admin'::admin_role_old
            WHEN 'technician' THEN 'operator'::admin_role_old
            WHEN 'viewer'     THEN 'operator'::admin_role_old
            ELSE 'operator'::admin_role_old
        END
    ),
    ALTER COLUMN role SET DEFAULT 'operator'::admin_role_old;

DROP TYPE admin_role;
ALTER TYPE admin_role_old RENAME TO admin_role;

-- +goose StatementEnd
