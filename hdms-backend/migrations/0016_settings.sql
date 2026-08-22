-- +goose Up
-- +goose StatementBegin

CREATE TABLE settings (
    id                           int PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    -- Policy
    block_on_overdue             boolean NOT NULL DEFAULT false,
    session_idle_timeout_seconds int NOT NULL DEFAULT 45,
    kiosk_sound_enabled          boolean NOT NULL DEFAULT true,
    low_stock_threshold          int NOT NULL DEFAULT 10,
    paper_backlog_hours          int NOT NULL DEFAULT 48,
    -- Label template
    sheet_width_mm               double precision NOT NULL DEFAULT 210.0,
    sheet_height_mm              double precision NOT NULL DEFAULT 297.0,
    label_columns                int NOT NULL DEFAULT 3,
    label_rows                   int NOT NULL DEFAULT 8,
    margin_top_mm                double precision NOT NULL DEFAULT 15.0,
    margin_left_mm               double precision NOT NULL DEFAULT 8.0,
    gutter_x_mm                  double precision NOT NULL DEFAULT 4.0,
    gutter_y_mm                  double precision NOT NULL DEFAULT 4.0,
    label_width_mm               double precision NOT NULL DEFAULT 50.0,
    label_height_mm              double precision NOT NULL DEFAULT 25.0,
    -- Slip template
    hospital_name                text NOT NULL DEFAULT 'HITO HOSPITAL',
    page_ref_format              text NOT NULL DEFAULT 'YYYY-MM-DD p.NNN',
    slip_rows_per_page           int NOT NULL DEFAULT 20,
    slip_columns                 text[] NOT NULL DEFAULT ARRAY['#', 'ASSET TAG', 'BORROWER NAME', 'EMPLOYEE NUMBER', 'DEPT', 'OUT time', 'IN time', 'SIGN'],
    -- Audit metadata
    updated_at                   timestamptz NOT NULL DEFAULT now(),
    updated_by                   text NOT NULL DEFAULT 'system'
);

INSERT INTO settings (id) VALUES (1);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS settings;

-- +goose StatementEnd
