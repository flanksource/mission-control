-- +goose Up
-- +goose StatementBegin

CREATE TABLE responder_queue (
    id UUID DEFAULT generate_ulid() PRIMARY KEY,
    responder_id UUID NOT NULL,
    error TEXT NULL,
    created_at timestamp NOT NULL DEFAULT now(),
    updated_at timestamp NOT NULL DEFAULT now()
);

CREATE OR REPLACE FUNCTION insert_responder_queue()
RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO responder_queue(responder_id) VALUES (new.id);
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
