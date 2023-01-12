CREATE TABLE IF NOT EXISTS identities();

-- Insert identities in people table
CREATE OR REPLACE FUNCTION insert_identity_to_people()
RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO people(id, name, email)
    VALUES (NEW.id, concat(NEW.traits::json->'name'->>'first', ' ', NEW.traits::json->'name'->>'last'), NEW.traits::json->>'email');
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE OR REPLACE TRIGGER identity_to_people
    AFTER INSERT
    ON identities
    FOR EACH ROW
    EXECUTE PROCEDURE insert_identity_to_people();


-- Get current user or fallback to system user
CREATE OR REPLACE FUNCTION get_current_user()
RETURNS UUID AS $$
DECLARE
    output UUID;
BEGIN
    SELECT id FROM people INTO output WHERE name = 'System' ORDER BY created_at ASC LIMIT 1;
    RETURN output;
END
$$ LANGUAGE plpgsql;
