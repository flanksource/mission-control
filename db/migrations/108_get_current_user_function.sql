-- +goose Up
-- +goose StatementBegin
---

-- Get current user or fallback to system user
CREATE OR REPLACE FUNCTION get_current_user()
RETURNS UUID AS $$
DECLARE
    output UUID;
BEGIN
    SELECT id FROM people INTO output WHERE name = 'System' ORDER BY created_at ASC LIMIT 1;
    RETURN output;
END
$$ LANGUAGE plpgsql;

-- +goose StatementEnd
