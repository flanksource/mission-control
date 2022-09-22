-- +goose Up

CREATE TABLE  IF NOT EXISTS integrations (
  id UUID DEFAULT generate_ulid() PRIMARY KEY,
  name TEXT NOT NULL,
  icon TEXT NULL,
  spec JSONB null,
  source TEXT NULL,
  created_by UUID NOT NULL,
  created_at timestamp NOT NULL DEFAULT now(),
  updated_at timestamp NOT NULL DEFAULT now(),
  FOREIGN KEY (created_by) REFERENCES people (id)
);

-- +goose Down
DROP TABLE IF EXISTS integrations;
