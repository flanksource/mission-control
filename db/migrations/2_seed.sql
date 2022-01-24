-- +goose Up
-- +goose StatementBegin
---
CREATE TABLE person (
  id UUID DEFAULT generate_ulid() PRIMARY KEY,
  name TEXT NOT NULL,
  avatar TEXT NULL,
  team TEXT NULL,
  organization TEXT NULL,
  title TEXT NULL,
  email TEXT NULL,
  phone TEXT NULL,
  properties jsonb null,
  created_at timestamp NOT NULL DEFAULT now(),
  updated_at timestamp NOT NULL DEFAULT now()
);
INSERT INTO person (name)
VALUES ('System');
---
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
  created_at timestamp NOT NULL DEFAULT now(),
  updated_at timestamp NOT NULL DEFAULT now(),
  FOREIGN KEY (created_by) REFERENCES person (id),
  FOREIGN KEY (commander_id) REFERENCES person (id),
  FOREIGN KEY (communicator_id) REFERENCES person (id)
);

---
CREATE TABLE hypothesis (
  id UUID DEFAULT generate_ulid() PRIMARY KEY,
  created_by UUID NOT NULL,
  incident_id UUID NOT NULL,
  parent_id UUID NULL,
  type TEXT NOT NULL CHECK (type IN ('root', 'factor', 'solution')),
  title TEXT NOT NULL,
  status TEXT NOT NULL,
  created_at timestamp NOT NULL DEFAULT now(),
  updated_at timestamp NOT NULL DEFAULT now(),
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
  hypothesis_id UUID NULL,
  created_at timestamp NOT NULL DEFAULT now(),
  updated_at timestamp NOT NULL DEFAULT now(),
  FOREIGN KEY (created_by) REFERENCES person(id),
  FOREIGN KEY (incident_id) REFERENCES incident(id),
  FOREIGN KEY (hypothesis_id) REFERENCES hypothesis(id)
);
---
CREATE TABLE evidence (
  id UUID DEFAULT generate_ulid() PRIMARY KEY,
  description TEXT NOT NULL,
  hypothesis_id UUID NOT NULL,
  created_by UUID NOT NULL,
  type TEXT NOT NULL CHECK (
    type IN (
      'metric',
      'log',
      'trace',
      'health',
      'url',
      'other'
    )
  ),
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
  type TEXT NOT NULL CHECK (type IN ('person', 'system', 'group')),
  person_id UUID NULL,
  properties json null,
  acknowledge_time timestamp NULL,
  signoff_time timestamp NULL,
  created_by UUID NOT NULL,
  created_at timestamp NOT NULL DEFAULT now(),
  updated_at timestamp NOT NULL DEFAULT now(),
  FOREIGN KEY (person_id) REFERENCES person(id),
  FOREIGN KEY (incident_id) REFERENCES incident(id),
  FOREIGN KEY (created_by) REFERENCES person(id)
);


-- +goose StatementEnd
