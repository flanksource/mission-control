CREATE TABLE incident_rule (
  id UUID DEFAULT generate_ulid() PRIMARY KEY,
  name TEXT NOT NULL,
  spec JSONB null,
  source TEXT NULL, -- The CRD source of the rule, if specified the rule cannot be edited via API
  created_at timestamp NOT NULL DEFAULT now(),
  updated_at timestamp NOT NULL DEFAULT now()
);

CREATE TABLE incident (
  id UUID DEFAULT generate_ulid() PRIMARY KEY,
  title TEXT NOT NULL,
  created_by UUID NOT NULL,
  commander_id UUID NULL,
  communicator_id UUID NULL,
  severity int not null,
  description TEXT NOT NULL,
  type TEXT NOT NULL,
  status TEXT NOT NULL,
  acknowledged timestamp NULL,
  resolved timestamp NULL,
  closed timestamp NULL,
  created_at timestamp NOT NULL DEFAULT now(),
  updated_at timestamp NOT NULL DEFAULT now(),
  FOREIGN KEY (created_by) REFERENCES person (id),
  FOREIGN KEY (commander_id) REFERENCES person (id),
  FOREIGN KEY (communicator_id) REFERENCES person (id)
);

CREATE TABLE incident_history (
  id UUID DEFAULT generate_ulid() PRIMARY KEY,
  incident_id UUID NOT NULL,
  created_by UUID NOT NULL,
  type TEXT NULL,
  description text NOT NULL,
  hypothesis_id UUID NULL,
  created_at timestamp NOT NULL DEFAULT now(),
  updated_at timestamp NOT NULL DEFAULT now(),
  FOREIGN KEY (created_by) REFERENCES person(id),
  FOREIGN KEY (incident_id) REFERENCES incident(id),
  FOREIGN KEY (hypothesis_id) REFERENCES hypothesis(id)
);

---
CREATE TABLE hypothesis (
  id UUID DEFAULT generate_ulid() PRIMARY KEY,
  created_by UUID NOT NULL,
  incident_id UUID NOT NULL,
  parent_id UUID NULL,
  owner UUID NULL,
  team_id UUID NULL,
  type TEXT NOT NULL CHECK (type IN ('root', 'factor', 'solution')),
  title TEXT NOT NULL,
  status TEXT NOT NULL,
  created_at timestamp NOT NULL DEFAULT now(),
  updated_at timestamp NOT NULL DEFAULT now(),
  FOREIGN KEY (owner) REFERENCES responder(id),
  FOREIGN KEY (team_id) REFERENCES team(id),
  FOREIGN KEY (created_by) REFERENCES person(id),
  FOREIGN KEY (incident_id) REFERENCES incident(id),
  FOREIGN KEY (parent_id) REFERENCES hypothesis(id)
);

---
CREATE TABLE comment (
  id UUID DEFAULT generate_ulid() PRIMARY KEY,
  created_by UUID NOT NULL ,
  comment text NOT NULL,
  incident_id UUID NOT NULL,
  responder_id UUID NULL,
  hypothesis_id UUID NULL,
  read smallint[] NULL,
  created_at timestamp NOT NULL DEFAULT now(),
  updated_at timestamp NOT NULL DEFAULT now(),
  FOREIGN KEY (created_by) REFERENCES person(id),
  FOREIGN KEY (incident_id) REFERENCES incident(id),
  FOREIGN KEY (hypothesis_id) REFERENCES hypothesis(id),
 	FOREIGN KEY (responder_id) REFERENCES responder(id)
);

CREATE TABLE comment_queue (
 id UUID DEFAULT generate_ulid() PRIMARY KEY,
 comment_id UUID NOT NULL,
 responder_id UUID NOT NULL,
 error TEXT NULL,
 created_at timestamp NOT NULL DEFAULT now(),
 updated_at timestamp NOT NULL DEFAULT now(),
)

---
CREATE TABLE evidence (
  id UUID DEFAULT generate_ulid() PRIMARY KEY,
  description TEXT NOT NULL,
  hypothesis_id UUID NOT NULL,
  created_by UUID NOT NULL,
  type TEXT NOT NULL,
  evidence jsonb null,
  properties jsonb null,
  created_at timestamp NOT NULL DEFAULT now(),
  updated_at timestamp NOT NULL DEFAULT now(),
  FOREIGN KEY (created_by) REFERENCES person(id),
  FOREIGN KEY (hypothesis_id) REFERENCES hypothesis(id)
);

---
CREATE TABLE responder (
  id UUID DEFAULT generate_ulid() PRIMARY KEY,
  incident_id UUID NOT NULL,
  type TEXT NOT NULL,
  index smallint NULL, -- The index at which the responder was added in the incident, used for read status tracking
  person_id UUID NULL,
  team_id UUID NULL,
  external_id TEXT NULL, -- A unique identifier for the responder in the external system e.g. Jira ticket id
  properties jsonb null,
  acknowledged timestamp NULL,
  reoslved timestamp NULL,
  closed timestamp NULL,
  created_by UUID NOT NULL,
  created_at timestamp NOT NULL DEFAULT now(),
  updated_at timestamp NOT NULL DEFAULT now(),
  FOREIGN KEY (person_id) REFERENCES person(id),
  FOREIGN KEY (team_id) REFERENCES team(id),
  FOREIGN KEY (incident_id) REFERENCES incident(id),
  FOREIGN KEY (created_by) REFERENCES person(id)
);
