-- +goose Up
-- +goose StatementBegin

-- Event queue for events that need to be processed in the background, once succesfully processed the event is removed from the queue
CREATE TABLE event_queue (
    id UUID DEFAULT generate_ulid() PRIMARY KEY,
    event TEXT NOT NULL,
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
    INSERT INTO event_queue(event, properties) VALUES ('responder.create', jsonb_build_object('type', 'responder', 'id', NEW.id));
    NOTIFY event_queue_updates, 'update';
    RETURN NEW;
END
$$ LANGUAGE plpgsql;


CREATE TRIGGER responder_enqueue
    AFTER INSERT
    ON responders
    FOR EACH ROW
    EXECUTE PROCEDURE insert_responder_in_event_queue();

-- +goose StatementEnd
