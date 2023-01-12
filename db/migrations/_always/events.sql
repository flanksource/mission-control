-- Insert responder updates in event_queue
CREATE OR REPLACE FUNCTION insert_responder_in_event_queue()
RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO event_queue(name, properties) VALUES ('responder.create', jsonb_build_object('type', 'responder', 'id', NEW.id));
    NOTIFY event_queue_updates, 'update';
    RETURN NEW;
END
$$ LANGUAGE plpgsql;


CREATE  OR REPLACE TRIGGER responder_enqueue
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

CREATE  OR REPLACE TRIGGER comment_enqueue
    AFTER INSERT
    ON comments
    FOR EACH ROW
    EXECUTE PROCEDURE insert_comment_in_event_queue();
