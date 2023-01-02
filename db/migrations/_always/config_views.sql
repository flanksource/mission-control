-- TODO stop the recursion once max_depth is reached.level <= max_depth;
CREATE OR REPLACE FUNCTION lookup_config_children(id text, max_depth int)
RETURNS TABLE(
    child_id UUID,
    parent_id UUID,
    level int
) AS $$
BEGIN
    IF max_depth < 0 THEN
        max_depth = 10;
    END IF;
    RETURN QUERY
        WITH RECURSIVE children AS (
            SELECT config_items.id as child_id, config_items.parent_id, 0 as level
            FROM config_items
            WHERE config_items.id = $1::uuid
            UNION ALL
            SELECT m.id as child_id, m.parent_id, c.level + 1 as level
            FROM config_items m
            JOIN children c ON m.parent_id = c.child_id
        )
        SELECT children.child_id, children.parent_id, children.level FROM children
        WHERE children.level <= max_depth;
END;
$$
language plpgsql;

CREATE OR REPLACE FUNCTION lookup_config_relations(config_id text)
RETURNS TABLE (
    id UUID
) AS $$
BEGIN
    RETURN QUERY
        SELECT cr.related_id AS id FROM config_relationships cr WHERE cr.config_id = $1::UUID
        UNION
        SELECT cr.config_id as id FROM config_relationships cr WHERE cr.related_id = $1::UUID;
END;
$$
language plpgsql;
