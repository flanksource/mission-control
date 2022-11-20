-- CREATE OR REPLACE VIEW  checks_by_component AS

--       SELECT check_component_relationships.component_id, json_agg(checks) from checks
--             LEFT JOIN check_component_relationships ON checks.id = check_component_relationships.check_id
--             WHERE    check_component_relationships.deleted_at is null
--             GROUP BY check_component_relationships.component_id;


-- CREATE OR REPLACE VIEW components_flat AS
-- 	SELECT components.id, components.type, components.name, jsonb_set_lax(to_jsonb(components),'{checks}',
-- 			(SELECT json_agg(checks) from checks LEFT JOIN check_component_relationships ON checks.id = check_component_relationships.check_id WHERE check_component_relationships.component_id = components.id AND check_component_relationships.deleted_at is null   GROUP BY check_component_relationships.component_id) :: jsonb
-- 			 ) :: jsonb as components from components where components.deleted_at is null;

-- select * from components_flat


CREATE OR REPLACE function lookup_component_by_property(text, text)
returns setof components
as
$$
begin
  return query
    select * from components where deleted_at is null AND properties != 'null' and name in (select name  from components,jsonb_array_elements(properties) property where properties != 'null' and  property is not null and  property->>'name' = $1 and property->>'text' = $2);
end;
$$
language plpgsql;

DROP VIEW IF EXISTS incidents_by_component;
CREATE OR REPLACE VIEW incidents_by_component AS
  SELECT DISTINCT incidents.title,incidents.id, incidents.created_at, incidents."type", incidents.status, incidents.severity, component_id FROM evidences
  INNER join hypotheses on evidences.hypothesis_id = hypotheses.id
  INNER JOIN incidents on hypotheses.incident_id = incidents.id
  WHERE component_id is not null;



DROP VIEW IF EXISTS incidents_by_config;
CREATE OR REPLACE VIEW incidents_by_config AS
  SELECT DISTINCT incidents.title, incidents.id, incidents.created_at, incidents."type", incidents.status,  incidents.severity, config_id FROM evidences
  INNER join hypotheses on evidences.hypothesis_id = hypotheses.id
  INNER JOIN incidents on hypotheses.incident_id = incidents.id
  WHERE evidences.config_id is not null;


CREATE OR REPLACE VIEW changes_by_component AS
	SELECT config_changes.config_id, change_type, config_changes.created_at, component_id
  from config_changes
  INNER JOIN config_component_relationships relations on relations.config_id = config_changes.config_id;


CREATE OR REPLACE VIEW analysis_by_component AS
	SELECT config_analysis.config_id, analyzer, analysis_type, severity, status, first_observed, last_observed, component_id
  from config_analysis
  INNER JOIN config_component_relationships relations on relations.config_id = config_analysis.config_id;


CREATE OR REPLACE VIEW component_names AS
      SELECT id, external_id, type, name, created_at, updated_at, icon, parent_id FROM components WHERE deleted_at is null AND hidden != true ORDER BY name, external_id  ;

CREATE OR REPLACE VIEW component_labels AS
      SELECT d.key, d.value FROM components JOIN json_each_text(labels::json) d on true GROUP BY d.key, d.value ORDER BY key, value;

CREATE OR REPLACE VIEW check_names AS
      SELECT id, canary_id, type, name, status FROM checks where deleted_at is null AND silenced_at is null ORDER BY name;

CREATE OR REPLACE VIEW check_labels AS
      SELECT d.key, d.value FROM checks JOIN json_each_text(labels::json) d on true GROUP BY d.key, d.value ORDER BY key, value;


