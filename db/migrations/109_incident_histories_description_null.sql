-- +goose Up
-- +goose StatementBegin
---

ALTER TABLE incident_histories ALTER COLUMN description DROP NOT NULL;

-- +goose StatementEnd
