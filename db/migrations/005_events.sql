-- +goose Up
-- +goose StatementBegin

-- Event queue for events that need to be processed in the background, once succesfully processed the event is removed from the queue
CREATE TABLE event_queue (
    id UUID DEFAULT generate_ulid() PRIMARY KEY,
    name TEXT NOT NULL,
    properties jsonb NULL,
    error TEXT NULL,
    created_at timestamp NOT NULL DEFAULT now(),
    last_attempt timestamp NULL,
    attempts int DEFAULT 0 -- Keep a count of attempts and stop retrying after N attempts
);


-- Insert responder updates in event_queue
CREATE OR REPLACE FUNCTION insert_responder_in_event_queue()
RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO event_queue(name, properties) VALUES ('responder.create', jsonb_build_object('type', 'responder', 'id', NEW.id));
    NOTIFY event_queue_updates, 'update';
    RETURN NEW;
END
$$ LANGUAGE plpgsql;


CREATE TRIGGER responder_enqueue
    AFTER INSERT
    ON responders
    FOR EACH ROW
    EXECUTE PROCEDURE insert_responder_in_event_queue();

-- Insert comment updates in event_queue
CREATE OR REPLACE FUNCTION insert_comment_in_event_queue()
RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO event_queue(name, properties) VALUES ('comment.create', jsonb_build_object('type', 'comment', 'id', NEW.id, 'body', NEW.comment));
    NOTIFY event_queue_updates, 'update';
    RETURN NEW;
END
$$ LANGUAGE plpgsql;


CREATE TRIGGER comment_enqueue
    AFTER INSERT
    ON comments
    FOR EACH ROW
    EXECUTE PROCEDURE insert_comment_in_event_queue();

-- +goose StatementEnd

-- +goose Down

-- DROP TRIGGER responder_enqueue on responders;
DROP TABLE event_queue;
