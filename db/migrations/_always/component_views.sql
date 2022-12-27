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


CREATE OR REPLACE function lookup_configs_by_component(id text)
returns table (
  config_id UUID,
  name TEXT,
  type TEXT,
  icon TEXT,
  role TEXT
)
as
$$
begin
  RETURN QUERY
	  SELECT config_items.id as config_id, config_items.name, config_items.config_type, config_items.icon, 'left' as role
	  FROM config_component_relationships
	  INNER JOIN  config_items on config_items.id = config_component_relationships.config_id
	  WHERE config_component_relationships.component_id = $1::uuid;
end;
$$
language plpgsql

CREATE OR REPLACE function lookup_changes_by_component(id text)
RETURNS SETOF config_changes as
$$
begin
  RETURN QUERY select * from config_changes where config_id in (select config_id from lookup_configs_by_component($1));
end;
$$
language plpgsql;

CREATE OR REPLACE function lookup_components_by_config(id text)
returns table (
  component_id UUID,
  name TEXT,
  type TEXT,
  icon TEXT,
  role TEXT
)
as
$$
begin
  RETURN QUERY
	  SELECT components.id as component_id , components.name, components.type, components.icon, 'left' as role
	  FROM config_component_relationships
	  INNER JOIN  components on components.id = config_component_relationships.component_id
	  WHERE config_component_relationships.config_id = $1::uuid;
end;
$$
language plpgsql;

DROP function lookup_related_configs;
CREATE OR REPLACE function lookup_related_configs(id text)
returns table (
  config_id UUID,
  name TEXT,
  type TEXT,
  icon TEXT,
  role TEXT,
  relation TEXT
)
as
$$
begin

  RETURN QUERY
	  SELECT parent.id as config_id, parent.name, parent.config_type, parent.icon, 'parent' as role, null
	  FROM config_items
	  INNER JOIN  config_items parent on config_items.parent_id = parent.id
	  WHERE config_items.id = $1::uuid
	UNION
		  SELECT config_items.id as config_id, config_items.name, config_items.config_type, config_items.icon, 'left' as role, config_relationships.relation
		  FROM config_relationships
		  INNER JOIN  config_items on config_items.id = config_relationships.related_id
		  WHERE config_relationships.config_id = $1::uuid
	UNION
		  SELECT config_items.id as config_id, config_items.name, config_items.config_type, config_items.icon, 'right' as role , config_relationships.relation
		  FROM config_relationships
		  INNER JOIN  config_items on config_items.id = config_relationships.config_id
		  WHERE config_relationships.related_id = $1::uuid;
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

DROP VIEW IF EXISTS changes_by_component;
CREATE OR REPLACE VIEW changes_by_component AS
	SELECT config_changes.config_id, configs.name, configs.config_type, configs.external_type, change_type,
         config_changes.created_at,config_changes.created_by, config_changes.id as change_id, config_changes.severity, component_id
  FROM config_changes
  INNER JOIN config_component_relationships relations on relations.config_id = config_changes.config_id
  INNER JOIN config_items  configs on configs.id = config_changes.config_id;


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


