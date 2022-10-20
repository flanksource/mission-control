-- +goose Up
-- +goose StatementBegin
---

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

-- Insert incident responder creations in incident_histories
CREATE OR REPLACE FUNCTION insert_responder_created_in_incident_history()
RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO incident_histories(incident_id, created_by, type, description) VALUES (NEW.incident_id, NEW.created_by, 'responder.created', NEW.id);
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
    INSERT INTO incident_histories(incident_id, created_by, type, hypothesis_id, description) VALUES (incident_id, NEW.created_by, 'evidence.created', NEW.hypothesis_id, NEW.id);
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
    INSERT INTO incident_histories(incident_id, created_by, type, description) VALUES (NEW.incident_id, NEW.created_by, 'responder.responded', NEW.id);
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE TRIGGER responder_comment_to_incident_history
    AFTER INSERT
    ON comments
    FOR EACH ROW
    WHEN (NEW.responder_id IS NOT NULL)
    EXECUTE PROCEDURE insert_incident_status_update_in_incident_history();

-- Insert hypothesis status updates in incident_histories
CREATE OR REPLACE FUNCTION insert_hypothesis_status_update_in_incident_history()
RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO incident_histories(incident_id, created_by, type, description, hypothesis_id) VALUES (NEW.incident_id, NEW.created_by, 'hypothesis_status.updated', NEW.status, NEW.id);
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
