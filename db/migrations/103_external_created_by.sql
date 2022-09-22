-- +goose Up

ALTER TABLE comments ADD COLUMN  IF NOT EXISTS external_created_by TEXT NULL;
