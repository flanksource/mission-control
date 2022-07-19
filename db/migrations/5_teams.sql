CREATE TABLE team (
  id UUID DEFAULT generate_ulid() PRIMARY KEY,
  name TEXT NOT NULL,
  icon TEXT NULL,
  spec JSONB null,
  source TEXT NULL, -- The CRD source of the rule, if specified the rule cannot be edited via API
  created_by UUID NOT NULL,
  created_at timestamp NOT NULL DEFAULT now(),
  updated_at timestamp NOT NULL DEFAULT now(),
  FOREIGN KEY (created_by) REFERENCES person(id)
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
