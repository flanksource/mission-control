-- +goose Up
-- +goose StatementBegin


CREATE OR REPLACE FUNCTION insert_responder_queue()
RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO event_queue(event,properties) VALUES ('Responder.create', 	json_build_object('responder_id',new.id));
    SELECT pg_notify("responder_updates", new.id);
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE TRIGGER responder_enqueue
    AFTER INSERT
    ON responder
    FOR EACH ROW
    EXECUTE PROCEDURE insert_responder_queue();

-- +goose StatementEnd
