-- +goose Up
-- +goose StatementBegin
---
CREATE TABLE person (
  id UUID DEFAULT generate_ulid() PRIMARY KEY,
  name TEXT NOT NULL,
  avatar TEXT NULL,
  team_id UUID NULL,
  organization TEXT NULL,
  title TEXT NULL,
  email TEXT NULL,
  phone TEXT NULL,
  properties jsonb null,
  created_at timestamp NOT NULL DEFAULT now(),
  updated_at timestamp NULL
);

INSERT INTO person (name) VALUES ('System');

CREATE TABLE event_queue (
    id UUID DEFAULT generate_ulid() PRIMARY KEY,
    event TEXT NOT NULL,
    properties jsonb null,
    error TEXT NULL,
    created_at timestamp NOT NULL DEFAULT now(),
    attempts int,
    last_attempt timestamp NULL
);

-- +goose StatementEnd
