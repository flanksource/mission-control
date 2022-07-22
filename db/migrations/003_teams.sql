-- +goose Up
-- +goose StatementBegin
---

CREATE TABLE team (
  id UUID DEFAULT generate_ulid() PRIMARY KEY,
  name TEXT NOT NULL,
  icon TEXT NULL,
  spec JSONB null,
  source TEXT NULL,
  created_by UUID NOT NULL,
  created_at timestamp NOT NULL DEFAULT now(),
  updated_at timestamp NOT NULL DEFAULT now()
);

CREATE TABLE person (
  id UUID DEFAULT generate_ulid() PRIMARY KEY,
  name TEXT NOT NULL,
  avatar TEXT NULL,
  team_id UUID NULL, -- every team is also a person
  organization TEXT NULL,
  title TEXT NULL,
  email TEXT NULL,
  phone TEXT NULL,
  properties jsonb null,
  created_at timestamp NOT NULL DEFAULT now(),
  updated_at timestamp NULL,
  FOREIGN KEY (team_id) REFERENCES team(id)
);


INSERT INTO person (name) VALUES ('System');

ALTER TABLE team ADD FOREIGN KEY (created_by) REFERENCES person(id);

CREATE TABLE team_members (
  team_id UUID NOT NULL,
  person_id UUID NOT NULL,
  PRIMARY KEY (team_id, person_id),
  FOREIGN KEY (team_id) REFERENCES team(id),
  FOREIGN KEY (person_id) REFERENCES person(id)
);

CREATE TABLE team_components (
  team_id UUID NOT NULL,
  component_id UUID NOT NULL,
  role TEXT NULL,
  PRIMARY KEY (team_id, component_id),
  FOREIGN KEY (team_id) REFERENCES team(id)
  -- FOREIGN KEY (component_id) REFERENCES component(id)
);

-- +goose StatementEnd
