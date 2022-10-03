-- +goose Up

ALTER TABLE evidences ADD COLUMN  IF NOT EXISTS definition_of_done boolean;
ALTER TABLE evidences ADD COLUMN  IF NOT EXISTS factor boolean;
ALTER TABLE evidences ADD COLUMN  IF NOT EXISTS mitigator boolean;
ALTER TABLE evidences ADD COLUMN  IF NOT EXISTS config_id UUID;
ALTER TABLE evidences ADD COLUMN  IF NOT EXISTS config_change_id UUID;
ALTER TABLE evidences ADD COLUMN  IF NOT EXISTS config_analysis_id UUID;
ALTER TABLE evidences ADD COLUMN  IF NOT EXISTS component_id UUID;
ALTER TABLE evidences ADD COLUMN  IF NOT EXISTS check_id UUID;

ALTER TABLE evidences ADD FOREIGN KEY (config_id) REFERENCES config_items(id);
ALTER TABLE evidences ADD FOREIGN KEY (component_id) REFERENCES components(id);
ALTER TABLE evidences ADD FOREIGN KEY (check_id) REFERENCES checks(id);
ALTER TABLE evidences ADD FOREIGN KEY (config_change_id) REFERENCES config_changes(id);
ALTER TABLE evidences ADD FOREIGN KEY (config_analysis_id) REFERENCES config_analysis(id);

ALTER TABLE incidents ADD COLUMN  IF NOT EXISTS incident_rule_id UUID;
ALTER TABLE incidents ADD FOREIGN KEY (incident_rule_id) REFERENCES incident_rules (id);


CREATE TABLE incident_relationships (
  incident_id UUID NOT NULL,
  related_id UUID NOT NULL,
  relationship TEXT NOT NULL,
  created_at timestamp NOT NULL DEFAULT now(),
  updated_at timestamp NOT NULL DEFAULT now(),
  FOREIGN KEY (incident_id) REFERENCES incidents (id),
  FOREIGN KEY (related_id) REFERENCES incidents (id)
);
