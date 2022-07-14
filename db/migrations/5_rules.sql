-- +goose Up
-- +goose StatementBegin
---
CREATE TABLE incident_rule (
  id UUID DEFAULT generate_ulid() PRIMARY KEY,
  name TEXT NOT NULL,
  spec JSONB null,
  source TEXT NULL,
  created_at timestamp NOT NULL DEFAULT now(),
  updated_at timestamp NOT NULL DEFAULT now()
);


ALTER TABLE responder ADD COLUMN acknowledged timestamp NULL;
ALTER TABLE responder ADD COLUMN resolved timestamp NULL;
ALTER TABLE responder ADD COLUMN closed timestamp NULL;
ALTER TABLE responder ADD COLUMN external_id TEXT NULL;

ALTER TABLE incident ADD COLUMN acknowledged timestamp NULL;
ALTER TABLE incident ADD COLUMN resolved timestamp NULL;
ALTER TABLE incident ADD COLUMN closed timestamp NULL;

ALTER TABLE responder DROP COLUMN acknowledge_time;
ALTER TABLE responder DROP COLUMN signoff_time;

ALTER TABLE comment ADD COLUMN responder_id UUID NULL;
ALTER TABLE comment ADD	FOREIGN KEY (responder_id) REFERENCES responder(id);


CREATE TABLE incident_history (
  id UUID DEFAULT generate_ulid() PRIMARY KEY,
  incident_id UUID NOT NULL,
  created_by UUID NOT NULL,
  type TEXT NULL,
  description text NOT NULL,
  hypothesis_id UUID NULL,
  created_at timestamp NOT NULL DEFAULT now(),
  updated_at timestamp NOT NULL DEFAULT now(),
  FOREIGN KEY (created_by) REFERENCES person(id),
  FOREIGN KEY (incident_id) REFERENCES incident(id),
  FOREIGN KEY (hypothesis_id) REFERENCES hypothesis(id)
);




-- +goose StatementEnd
