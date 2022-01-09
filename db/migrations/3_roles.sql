-- +goose Up
-- +goose StatementBegin
---
CREATE ROLE  postgrest_api;
GRANT SELECT, UPDATE, DELETE, INSERT ON ALL TABLES IN SCHEMA public TO postgrest_api;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, UPDATE, DELETE, INSERT ON TABLES TO postgrest_api;
-- +goose StatementEnd
