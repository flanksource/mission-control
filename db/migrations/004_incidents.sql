-- +goose Up
-- +goose StatementBegin
---

CREATE TABLE IF NOT EXISTS severities (
  id int,
  name text NOT NULL,
  aliases text[],
  icon text NULL
);

INSERT INTO severities (id, name, icon, aliases)
   VALUES (1, 'Critical', 'error',ARRAY ['P1']),
          (2, 'Blocker', 'error', ARRAY['P2']),
          (3, 'High', 'warning',ARRAY ['P3']),
          (4, 'Medium', 'info',ARRAY ['P4']),
          (5, 'Low', 'info', ARRAY['P4']);


CREATE TABLE incident_rules (
  id UUID DEFAULT generate_ulid() PRIMARY KEY,
  name TEXT NOT NULL,
  spec JSONB null,
  source TEXT NULL, -- The CRD source of the rule, if specified the rule cannot be edited via API
  created_by UUID NOT NULL,
  created_at timestamp NOT NULL DEFAULT now(),
  updated_at timestamp NOT NULL DEFAULT now(),
  FOREIGN KEY (created_by) REFERENCES people (id),
  UNIQUE (name)
);

CREATE TABLE incidents (
  id UUID DEFAULT generate_ulid() PRIMARY KEY,
  incident_rule_id UUID NULL,
  title TEXT NOT NULL,
  created_by UUID NOT NULL,
  commander_id UUID NULL,
  communicator_id UUID NULL,
  severity text not null,
  description TEXT NOT NULL,
  type TEXT NOT NULL,
  status TEXT NOT NULL,
  acknowledged timestamp NULL,
  resolved timestamp NULL,
  closed timestamp NULL,
  created_at timestamp NOT NULL DEFAULT now(),
  updated_at timestamp NOT NULL DEFAULT now(),
  FOREIGN KEY (created_by) REFERENCES people (id),
  FOREIGN KEY (commander_id) REFERENCES people (id),
  FOREIGN KEY (communicator_id) REFERENCES people (id),
  FOREIGN KEY (incident_rule_id) REFERENCES incident_rules (id)
);


CREATE TABLE incident_relationships (
  incident_id UUID NOT NULL,
  related_id UUID NOT NULL,
  relationship TEXT NOT NULL,
  created_at timestamp NOT NULL DEFAULT now(),
  updated_at timestamp NOT NULL DEFAULT now(),
  FOREIGN KEY (incident_id) REFERENCES incidents (id),
  FOREIGN KEY (related_id) REFERENCES incidents (id)
);

CREATE TABLE responders (
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
  FOREIGN KEY (person_id) REFERENCES people(id),
  FOREIGN KEY (team_id) REFERENCES teams(id),
  FOREIGN KEY (incident_id) REFERENCES incidents(id),
  FOREIGN KEY (created_by) REFERENCES people(id)
);

CREATE TABLE hypotheses (
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
  FOREIGN KEY (owner) REFERENCES responders(id),
  FOREIGN KEY (team_id) REFERENCES teams(id),
  FOREIGN KEY (created_by) REFERENCES people(id),
  FOREIGN KEY (incident_id) REFERENCES incidents(id),
  FOREIGN KEY (parent_id) REFERENCES hypotheses(id)
);

CREATE TABLE incident_histories (
  id UUID DEFAULT generate_ulid() PRIMARY KEY,
  incident_id UUID NOT NULL,
  created_by UUID NOT NULL,
  type TEXT NULL,
  description text NOT NULL,
  hypothesis_id UUID NULL,
  responder_id UUID NULL,
  evidence_id UUID NULL,
  comment_id UUID NULL,
  created_at timestamp NOT NULL DEFAULT now(),
  updated_at timestamp NOT NULL DEFAULT now(),
  FOREIGN KEY (created_by) REFERENCES people(id),
  FOREIGN KEY (incident_id) REFERENCES incidents(id),
  FOREIGN KEY (hypothesis_id) REFERENCES hypotheses(id),
  FOREIGN KEY (responder_id) REFERENCES responders(id),
  FOREIGN KEY (evidence_id) REFERENCES evidences(id),
  FOREIGN KEY (comment_id) REFERENCES comments(id)
);

CREATE TABLE comments (
  id UUID DEFAULT generate_ulid() PRIMARY KEY,
  created_by UUID NOT NULL ,
  comment text NOT NULL,
  external_id TEXT NULL, -- A unique identifier for the responder in the external system e.g. Jira ticket id
  external_created_by TEXT NULL, -- Author of the comment in the external system
  incident_id UUID NOT NULL,
  responder_id UUID NULL,
  hypothesis_id UUID NULL,
  read smallint[] NULL,
  created_at timestamp NOT NULL DEFAULT now(),
  updated_at timestamp NOT NULL DEFAULT now(),
  FOREIGN KEY (created_by) REFERENCES people(id),
  FOREIGN KEY (incident_id) REFERENCES incidents(id),
  FOREIGN KEY (responder_id) REFERENCES responders(id),
  FOREIGN KEY (hypothesis_id) REFERENCES hypotheses(id)
);

CREATE TABLE comment_responders (
  id UUID DEFAULT generate_ulid() PRIMARY KEY,
  comment_id UUID NOT NULL,
  responder_id UUID NOT NULL,
  external_id TEXT NULL, -- A unique identifier for the responder in the external system e.g. Jira ticket id
  created_at timestamp NOT NULL DEFAULT now(),
  updated_at timestamp NOT NULL DEFAULT now(),
  FOREIGN KEY (comment_id) REFERENCES comments(id),
  FOREIGN KEY (responder_id) REFERENCES responders(id)
);

---
CREATE TABLE evidences (
  id UUID DEFAULT generate_ulid() PRIMARY KEY,
  description TEXT NOT NULL,
  hypothesis_id UUID NOT NULL,
  config_id UUID NULL,
  config_change_id UUID null,
  config_analysis_id UUID null,
  component_id UUID null,
  check_id UUID null,
  definition_of_done boolean DEFAULT false, -- This indicates this item as needing to be fixed before closing the incident
  done boolean, -- The evidence is done / resolved
  factor boolean,
  mitigator boolean,
  created_by UUID NOT NULL,
  type TEXT NOT NULL,
  evidence jsonb null,
  properties jsonb null,
  script TEXT NULL,
  script_result TEXT NULL,
  created_at timestamp NOT NULL DEFAULT now(),
  updated_at timestamp NOT NULL DEFAULT now(),
  FOREIGN KEY (created_by) REFERENCES people(id),
  FOREIGN KEY (component_id) REFERENCES components(id),
  FOREIGN KEY (check_id) REFERENCES checks(id),
  FOREIGN KEY (config_id) REFERENCES config_items(id),
  FOREIGN KEY (config_change_id) REFERENCES config_changes(id),
  FOREIGN KEY (config_analysis_id) REFERENCES config_analysis(id),
  FOREIGN KEY (hypothesis_id) REFERENCES hypotheses(id)
);

-- Insert incident creations in incident_histories
CREATE OR REPLACE FUNCTION insert_incident_created_in_incident_history()
RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO incident_histories(incident_id, created_by, type, description) VALUES (NEW.id, NEW.created_by, 'incident.created', NEW.id);
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE TRIGGER incident_to_incident_history
    AFTER INSERT
    ON incidents
    FOR EACH ROW
    EXECUTE PROCEDURE insert_incident_created_in_incident_history();

-- Insert incident status updates in incident_histories
CREATE OR REPLACE FUNCTION insert_incident_status_update_in_incident_history()
RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO incident_histories(incident_id, created_by, type, description) VALUES (NEW.id, NEW.created_by, 'incident.status_updated', NEW.status);
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE TRIGGER incident_status_to_incident_history
    AFTER UPDATE
    ON incidents
    FOR EACH ROW
    WHEN (OLD.status IS DISTINCT FROM NEW.status)
    EXECUTE PROCEDURE insert_incident_status_update_in_incident_history();

-- Insert incident responder creations in incident_histories
CREATE OR REPLACE FUNCTION insert_responder_created_in_incident_history()
RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO incident_histories(incident_id, created_by, type, responder_id) VALUES (NEW.incident_id, NEW.created_by, 'responder.created', NEW.id);
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE TRIGGER responder_to_incident_history
    AFTER INSERT
    ON responders
    FOR EACH ROW
    EXECUTE PROCEDURE insert_responder_created_in_incident_history();

-- Insert evidence creations in incident_histories
CREATE OR REPLACE FUNCTION insert_evidence_created_in_incident_history()
RETURNS TRIGGER AS $$
DECLARE
incident_id UUID;
BEGIN
    SELECT hypotheses.incident_id INTO STRICT incident_id FROM hypotheses WHERE id = NEW.hypothesis_id;
    INSERT INTO incident_histories(incident_id, created_by, type, hypothesis_id, evidence_id) VALUES (incident_id, NEW.created_by, 'evidence.created', NEW.hypothesis_id, NEW.id);
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE TRIGGER evidence_to_incident_history
    AFTER INSERT
    ON evidences
    FOR EACH ROW
    EXECUTE PROCEDURE insert_evidence_created_in_incident_history();

-- Insert incident status updates in incident_histories
CREATE OR REPLACE FUNCTION insert_incident_status_update_in_incident_history()
RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO incident_histories(incident_id, created_by, type, description) VALUES (NEW.id, NEW.created_by, 'incident_status.updated', NEW.status);
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE TRIGGER incident_status_to_incident_history
    AFTER UPDATE
    ON incidents
    FOR EACH ROW
    WHEN (OLD.status IS DISTINCT FROM NEW.status)
    EXECUTE PROCEDURE insert_incident_status_update_in_incident_history();

-- Insert responder responses updates in incident_histories
CREATE OR REPLACE FUNCTION insert_responder_comment_in_incident_history()
RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO incident_histories(incident_id, created_by, type, comment_id) VALUES (NEW.incident_id, NEW.created_by, 'responder.commented', NEW.id);
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE TRIGGER responder_comment_to_incident_history
    AFTER INSERT
    ON comments
    FOR EACH ROW
    WHEN (NEW.responder_id IS NOT NULL)
    EXECUTE PROCEDURE insert_incident_status_update_in_incident_history();

-- Insert hypothesis creation updates in incident_histories
CREATE OR REPLACE FUNCTION insert_hypothesis_created_in_incident_history()
RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO incident_histories(incident_id, created_by, type, description, hypothesis_id) VALUES (NEW.incident_id, NEW.created_by, 'hypothesis.created', NEW.status, NEW.id);
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE TRIGGER hypothesis_status_to_incident_history
    AFTER INSERT
    ON hypotheses
    FOR EACH ROW
    EXECUTE PROCEDURE insert_hypothesis_created_in_incident_history();

-- Insert hypothesis status updates in incident_histories
CREATE OR REPLACE FUNCTION insert_hypothesis_status_update_in_incident_history()
RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO incident_histories(incident_id, created_by, type, description, hypothesis_id) VALUES (NEW.incident_id, NEW.created_by, 'hypothesis.status_updated', NEW.status, NEW.id);
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE TRIGGER hypothesis_status_to_incident_history
    AFTER UPDATE
    ON hypotheses
    FOR EACH ROW
    WHEN (OLD.status IS DISTINCT FROM NEW.status)
    EXECUTE PROCEDURE insert_hypothesis_status_update_in_incident_history();

-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS evidences;
DROP TABLE IF EXISTS comment_responders;
DROP TABLE IF EXISTS comments;
DROP TABLE IF EXISTS comment_responders;
DROP TABLE IF EXISTS incident_histories;
DROP TABLE IF EXISTS hypotheses;
DROP TABLE IF EXISTS responders;
DROP TABLE IF EXISTS incident_rules;
DROP TABLE IF EXISTS incidents;
DROP TABLE IF EXISTS severities;
