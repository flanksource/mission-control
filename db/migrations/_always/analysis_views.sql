-- analysis_by_component
CREATE OR REPLACE VIEW analysis_by_component AS
	SELECT config_analysis.config_id, analyzer, analysis_type, severity, status, first_observed, last_observed, component_id
  from config_analysis
  INNER JOIN config_component_relationships relations on relations.config_id = config_analysis.config_id;



-- analysis_by_config
CREATE OR REPLACE VIEW analysis_by_config AS
     WITH type_summary AS (
      SELECT summary.id, summary.type, json_object_agg(f.k, f.v) as json
      FROM (
          SELECT config_analysis.config_id AS id, analysis_type as type, json_build_object(severity, count(*)) AS severity_agg
          FROM config_analysis
		  WHERE status != 'resolved'
        	GROUP BY severity, analysis_type, config_id
       )
      AS summary, json_each(summary.severity_agg) AS f(k,v) GROUP BY summary.type, summary.id
    )

    SELECT id, jsonb_object_agg(key, value) as analysis FROM (
        SELECT id, json_object_agg(type,json) analysis from type_summary group by id, type) i
        ,json_each(analysis)
        GROUP BY id;




-- analysis_by_component
DROP VIEW IF EXISTS analysis_by_component;
CREATE OR REPLACE VIEW analysis_by_component AS
    SELECT config_analysis.config_id, configs.name, configs.config_type, configs.external_type, analysis_type,
         config_analysis.created_at,config_analysis.created_by,config_analysis.id as analysis_id, config_analysis.severity, component_id
  FROM config_analysis
  INNER JOIN config_component_relationships relations on relations.config_id = config_analysis.config_id
  INNER JOIN config_items configs on configs.id = config_analysis.config_id;

