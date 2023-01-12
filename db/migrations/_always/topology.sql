DROP FUNCTION IF EXISTS lookup_component_names;
CREATE OR REPLACE FUNCTION lookup_component_names(component_id uuid[])
RETURNS TABLE (
    names text[]
) AS $$
BEGIN
    RETURN QUERY
        SELECT array_agg(name) FROM components where id = any( component_id);

END;
$$
language plpgsql;


DROP VIEW IF EXISTS topology;
CREATE OR REPLACE VIEW topology AS
  WITH children AS (
    select relationship_id as id,  array_agg(component_id) as children from  component_relationships  where deleted_at is null group by id
  ),
  parents AS (
    select component_id as id,  array_agg(relationship_id) as parents from  component_relationships  where deleted_at is null group by id
  )

  SELECT components.*, checks, incidents, children.children, parents.parents from components
    LEFT JOIN check_summary_by_component on check_summary_by_component.component_id = components.id
    LEFT JOIN incident_summary_by_component on incident_summary_by_component.id = components.id
    LEFT JOIN children on children.id = components.id
    LEFT JOIN parents on parents.id = components.id
    WHERE components.deleted_at is null
