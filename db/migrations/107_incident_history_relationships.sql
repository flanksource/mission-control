-- +goose Up
-- +goose StatementBegin
---

ALTER TABLE incident_histories ADD COLUMN IF NOT EXISTS responder_id UUID;
ALTER TABLE incident_histories ADD COLUMN IF NOT EXISTS evidence_id UUID;
ALTER TABLE incident_histories ADD COLUMN IF NOT EXISTS comment_id UUID;

ALTER TABLE incident_histories ADD FOREIGN KEY (responder_id) REFERENCES responders(id);
ALTER TABLE incident_histories ADD FOREIGN KEY (evidence_id) REFERENCES evidences(id);
ALTER TABLE incident_histories ADD FOREIGN KEY (comment_id) REFERENCES comments(id);

-- +goose StatementEnd
