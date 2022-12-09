-- +goose Up
-- +goose StatementBegin

ALTER TABLE evidences ADD COLUMN script TEXT NULL;
ALTER TABLE evidences ADD COLUMN script_result TEXT NULL;
ALTER TABLE evidences ALTER COLUMN definition_of_done SET DEFAULT false;

-- +goose StatementEnd
