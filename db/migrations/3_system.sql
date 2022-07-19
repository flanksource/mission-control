CREATE TABLE person (
  id UUID DEFAULT generate_ulid() PRIMARY KEY,
  name TEXT NOT NULL,
  avatar TEXT NULL,
  team_id UUID NULL, -- every team is also a person
  organization TEXT NULL,
  title TEXT NULL,
  email TEXT NULL,
  phone TEXT NULL,
  properties jsonb null,
  created_at timestamp NOT NULL DEFAULT now(),
  updated_at timestamp NULL,
  FOREIGN KEY (team_id) REFERENCES team(id),
);
INSERT INTO person (name) VALUES ('System');


-- Event queue for events that need to be processed in the background, once succesfully processed the event is removed from the queue
CREATE TABLE event_queue (
 id UUID DEFAULT generate_ulid() PRIMARY KEY,
 properties jsonb null,
 error TEXT NULL,
 created_at timestamp NOT NULL DEFAULT now(),
 last_attempt timestamp NULL
);
