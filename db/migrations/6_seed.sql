-- +goose Up
-- +goose StatementBegin


-- Update the version_id to the latest version, so that the seed scripts can always reflect the desired state, and the versions post 6
-- are only used to migrate old databases, not setup new ones.
INSERT INTO incident_commander_db_version (version_id,is_applied,tstamp) values ('6',true, now());

-- +goose StatementEnd
