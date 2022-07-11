-- +goose Up
-- +goose StatementBegin
---
CREATE TABLE team (
  id UUID DEFAULT generate_ulid() PRIMARY KEY,
  name TEXT NOT NULL,
  icon TEXT NULL,
  spec JSONB null,
  source TEXT NULL,
  created_at timestamp NOT NULL DEFAULT now(),
  updated_at timestamp NOT NULL DEFAULT now()
);

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

ALTER TABLE hypothesis ADD  COLUMN owner UUID NULL;
ALTER TABLE hypothesis ADD	FOREIGN KEY (owner) REFERENCES person(id);

ALTER TABLE hypothesis ADD  COLUMN team_id UUID NULL;
ALTER TABLE hypothesis ADD	FOREIGN KEY (team_id) REFERENCES team(id);

ALTER TABLE person ADD  COLUMN team_id UUID NULL;
ALTER TABLE person ADD	FOREIGN KEY (team_id) REFERENCES team(id);


-- +goose StatementEnd
