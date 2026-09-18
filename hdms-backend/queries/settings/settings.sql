-- name: GetSettings :one
SELECT
    id,
    block_on_overdue,
    session_idle_timeout_seconds,
    kiosk_sound_enabled,
    low_stock_threshold,
    paper_backlog_hours,
    reservation_pre_window_minutes,
    reservation_expiry_grace_minutes,
    sheet_width_mm,
    sheet_height_mm,
    label_columns,
    label_rows,
    margin_top_mm,
    margin_left_mm,
    gutter_x_mm,
    gutter_y_mm,
    label_width_mm,
    label_height_mm,
    hospital_name,
    page_ref_format,
    slip_rows_per_page,
    slip_columns,
    updated_at,
    updated_by
FROM settings
WHERE id = 1;

-- name: UpdateSettings :one
UPDATE settings
SET
    -- Policy
    block_on_overdue = CASE WHEN @set_policy::boolean THEN @block_on_overdue::boolean ELSE block_on_overdue END,
    session_idle_timeout_seconds = CASE WHEN @set_policy::boolean THEN @session_idle_timeout_seconds::int ELSE session_idle_timeout_seconds END,
    kiosk_sound_enabled = CASE WHEN @set_policy::boolean THEN @kiosk_sound_enabled::boolean ELSE kiosk_sound_enabled END,
    low_stock_threshold = CASE WHEN @set_policy::boolean THEN @low_stock_threshold::int ELSE low_stock_threshold END,
    paper_backlog_hours = CASE WHEN @set_policy::boolean THEN @paper_backlog_hours::int ELSE paper_backlog_hours END,
    reservation_pre_window_minutes = CASE WHEN @set_policy::boolean THEN @reservation_pre_window_minutes::int ELSE reservation_pre_window_minutes END,
    reservation_expiry_grace_minutes = CASE WHEN @set_policy::boolean THEN @reservation_expiry_grace_minutes::int ELSE reservation_expiry_grace_minutes END,
    -- Label template
    sheet_width_mm = CASE WHEN @set_label_template::boolean THEN @sheet_width_mm::double precision ELSE sheet_width_mm END,
    sheet_height_mm = CASE WHEN @set_label_template::boolean THEN @sheet_height_mm::double precision ELSE sheet_height_mm END,
    label_columns = CASE WHEN @set_label_template::boolean THEN @label_columns::int ELSE label_columns END,
    label_rows = CASE WHEN @set_label_template::boolean THEN @label_rows::int ELSE label_rows END,
    margin_top_mm = CASE WHEN @set_label_template::boolean THEN @margin_top_mm::double precision ELSE margin_top_mm END,
    margin_left_mm = CASE WHEN @set_label_template::boolean THEN @margin_left_mm::double precision ELSE margin_left_mm END,
    gutter_x_mm = CASE WHEN @set_label_template::boolean THEN @gutter_x_mm::double precision ELSE gutter_x_mm END,
    gutter_y_mm = CASE WHEN @set_label_template::boolean THEN @gutter_y_mm::double precision ELSE gutter_y_mm END,
    label_width_mm = CASE WHEN @set_label_template::boolean THEN @label_width_mm::double precision ELSE label_width_mm END,
    label_height_mm = CASE WHEN @set_label_template::boolean THEN @label_height_mm::double precision ELSE label_height_mm END,
    -- Slip template
    hospital_name = CASE WHEN @set_slip_template::boolean THEN @hospital_name::text ELSE hospital_name END,
    page_ref_format = CASE WHEN @set_slip_template::boolean THEN @page_ref_format::text ELSE page_ref_format END,
    slip_rows_per_page = CASE WHEN @set_slip_template::boolean THEN @slip_rows_per_page::int ELSE slip_rows_per_page END,
    slip_columns = CASE WHEN @set_slip_template::boolean THEN @slip_columns::text[] ELSE slip_columns END,
    -- Audit metadata
    updated_at = now(),
    updated_by = @updated_by
WHERE id = 1
RETURNING
    id,
    block_on_overdue,
    session_idle_timeout_seconds,
    kiosk_sound_enabled,
    low_stock_threshold,
    paper_backlog_hours,
    reservation_pre_window_minutes,
    reservation_expiry_grace_minutes,
    sheet_width_mm,
    sheet_height_mm,
    label_columns,
    label_rows,
    margin_top_mm,
    margin_left_mm,
    gutter_x_mm,
    gutter_y_mm,
    label_width_mm,
    label_height_mm,
    hospital_name,
    page_ref_format,
    slip_rows_per_page,
    slip_columns,
    updated_at,
    updated_by;
