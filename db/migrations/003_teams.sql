-- +goose Up
-- +goose StatementBegin
---

CREATE TABLE  IF NOT EXISTS teams (
  id UUID DEFAULT generate_ulid() PRIMARY KEY,
  name TEXT NOT NULL,
  icon TEXT NULL,
  spec JSONB null,
  source TEXT NULL,
  created_by UUID NOT NULL,
  created_at timestamp NOT NULL DEFAULT now(),
  updated_at timestamp NOT NULL DEFAULT now()
);

CREATE TABLE  IF NOT EXISTS people (
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
  FOREIGN KEY (team_id) REFERENCES teams(id)
);


INSERT INTO people (name) VALUES ('System');

ALTER TABLE teams ADD FOREIGN KEY (created_by) REFERENCES people(id);

CREATE TABLE  IF NOT EXISTS team_members (
  team_id UUID NOT NULL,
  person_id UUID NOT NULL,
  PRIMARY KEY (team_id, person_id),
  FOREIGN KEY (team_id) REFERENCES teams(id),
  FOREIGN KEY (person_id) REFERENCES people(id)
);

CREATE TABLE  IF NOT EXISTS team_components (
  team_id UUID NOT NULL,
  component_id UUID NOT NULL,
  role TEXT NULL,
  selector_id TEXT,
  PRIMARY KEY (team_id, component_id),
  FOREIGN KEY (team_id) REFERENCES teams(id),
  FOREIGN KEY (component_id) REFERENCES components(id),
  UNIQUE (team_id, component_id, selector_id)
);

INSERT into incident_commander_db_version(version_id, tstamp, is_applied) VALUES(6, NOW(), true);

-- +goose StatementEnd

-- +goose Down

DROP TABLE IF EXISTS team_components;
DROP TABLE IF EXISTS team_members;
DROP TABLE IF EXISTS teams;
DROP TABLE IF EXISTS people;
