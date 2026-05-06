package pgschema

import (
	"context"
	"fmt"
	"sort"

	"gorm.io/gorm"
)

type Response struct {
	Tables   []Table `json:"tables"`
	Database string  `json:"database,omitempty"`
}

type Table struct {
	Name        string       `json:"name"`
	Schema      string       `json:"schema"`
	Type        string       `json:"type,omitempty"`
	Columns     []Column     `json:"columns"`
	Indexes     []Index      `json:"indexes,omitempty"`
	Constraints []Constraint `json:"constraints,omitempty"`
}

type Column struct {
	Name       string `json:"name"`
	DataType   string `json:"dataType"`
	Nullable   bool   `json:"nullable"`
	Ordinal    int    `json:"ordinal"`
	DefaultSQL string `json:"defaultSql,omitempty"`
}

type Index struct {
	Name       string `json:"name"`
	Definition string `json:"definition"`
}

type Constraint struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Definition string `json:"definition"`
}

func Introspect(ctx context.Context, db *gorm.DB) (*Response, error) {
	var database string
	if err := db.WithContext(ctx).Raw(`SELECT current_database()`).Scan(&database).Error; err != nil {
		return nil, fmt.Errorf("current database: %w", err)
	}
	var tables []tableRow
	if err := db.WithContext(ctx).Raw(`
SELECT n.nspname AS schema_name, c.relname AS table_name,
       CASE c.relkind
         WHEN 'r' THEN 'table'
         WHEN 'p' THEN 'partitioned_table'
         WHEN 'v' THEN 'view'
         WHEN 'm' THEN 'materialized_view'
         WHEN 'f' THEN 'foreign_table'
         ELSE c.relkind::text
       END AS relation_type
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE c.relkind IN ('r','p','v','m','f')
  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
  AND n.nspname NOT LIKE 'pg_toast%'
ORDER BY n.nspname, c.relname
`).Scan(&tables).Error; err != nil {
		return nil, fmt.Errorf("list relations: %w", err)
	}
	columns, err := scanColumns(ctx, db)
	if err != nil {
		return nil, err
	}
	indexes, err := scanIndexes(ctx, db)
	if err != nil {
		return nil, err
	}
	constraints, err := scanConstraints(ctx, db)
	if err != nil {
		return nil, err
	}

	type key struct{ schema, name string }
	out := make([]Table, 0, len(tables))
	for _, t := range tables {
		k := key{schema: t.Schema, name: t.Name}
		out = append(out, Table{
			Name:        t.Name,
			Schema:      t.Schema,
			Type:        t.Type,
			Columns:     nonNilColumns(columns[k]),
			Indexes:     nonNilIndexes(indexes[k]),
			Constraints: nonNilConstraints(constraints[k]),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Schema != out[j].Schema {
			return out[i].Schema < out[j].Schema
		}
		return out[i].Name < out[j].Name
	})
	return &Response{Tables: out, Database: database}, nil
}

func nonNilColumns(v []Column) []Column {
	if v == nil {
		return []Column{}
	}
	return v
}

func nonNilIndexes(v []Index) []Index {
	if v == nil {
		return []Index{}
	}
	return v
}

func nonNilConstraints(v []Constraint) []Constraint {
	if v == nil {
		return []Constraint{}
	}
	return v
}

type tableRow struct {
	Schema string `gorm:"column:schema_name"`
	Name   string `gorm:"column:table_name"`
	Type   string `gorm:"column:relation_type"`
}

type columnRow struct {
	Schema     string  `gorm:"column:schema_name"`
	Table      string  `gorm:"column:table_name"`
	Name       string  `gorm:"column:column_name"`
	DataType   string  `gorm:"column:data_type"`
	Nullable   bool    `gorm:"column:nullable"`
	Ordinal    int     `gorm:"column:ordinal_position"`
	DefaultSQL *string `gorm:"column:default_sql"`
}

type indexRow struct {
	Schema     string `gorm:"column:schema_name"`
	Table      string `gorm:"column:table_name"`
	Name       string `gorm:"column:index_name"`
	Definition string `gorm:"column:definition"`
}

type constraintRow struct {
	Schema     string `gorm:"column:schema_name"`
	Table      string `gorm:"column:table_name"`
	Name       string `gorm:"column:constraint_name"`
	Type       string `gorm:"column:constraint_type"`
	Definition string `gorm:"column:definition"`
}

func scanColumns(ctx context.Context, db *gorm.DB) (map[struct{ schema, name string }][]Column, error) {
	var rows []columnRow
	if err := db.WithContext(ctx).Raw(`
SELECT table_schema AS schema_name, table_name, column_name,
       COALESCE(udt_name, data_type) AS data_type,
       is_nullable = 'YES' AS nullable,
       ordinal_position,
       column_default AS default_sql
FROM information_schema.columns
WHERE table_schema NOT IN ('pg_catalog', 'information_schema')
ORDER BY table_schema, table_name, ordinal_position
`).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list columns: %w", err)
	}
	out := map[struct{ schema, name string }][]Column{}
	for _, r := range rows {
		k := struct{ schema, name string }{r.Schema, r.Table}
		out[k] = append(out[k], Column{
			Name:       r.Name,
			DataType:   r.DataType,
			Nullable:   r.Nullable,
			Ordinal:    r.Ordinal,
			DefaultSQL: strDeref(r.DefaultSQL),
		})
	}
	return out, nil
}

func scanIndexes(ctx context.Context, db *gorm.DB) (map[struct{ schema, name string }][]Index, error) {
	var rows []indexRow
	if err := db.WithContext(ctx).Raw(`
SELECT schemaname AS schema_name, tablename AS table_name, indexname AS index_name, indexdef AS definition
FROM pg_indexes
WHERE schemaname NOT IN ('pg_catalog', 'information_schema')
ORDER BY schemaname, tablename, indexname
`).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list indexes: %w", err)
	}
	out := map[struct{ schema, name string }][]Index{}
	for _, r := range rows {
		k := struct{ schema, name string }{r.Schema, r.Table}
		out[k] = append(out[k], Index{Name: r.Name, Definition: r.Definition})
	}
	return out, nil
}

func scanConstraints(ctx context.Context, db *gorm.DB) (map[struct{ schema, name string }][]Constraint, error) {
	var rows []constraintRow
	if err := db.WithContext(ctx).Raw(`
SELECT ns.nspname AS schema_name, rel.relname AS table_name, con.conname AS constraint_name,
       con.contype::text AS constraint_type,
       pg_get_constraintdef(con.oid) AS definition
FROM pg_constraint con
JOIN pg_class rel ON rel.oid = con.conrelid
JOIN pg_namespace ns ON ns.oid = rel.relnamespace
WHERE ns.nspname NOT IN ('pg_catalog', 'information_schema')
ORDER BY ns.nspname, rel.relname, con.conname
`).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list constraints: %w", err)
	}
	out := map[struct{ schema, name string }][]Constraint{}
	for _, r := range rows {
		k := struct{ schema, name string }{r.Schema, r.Table}
		out[k] = append(out[k], Constraint{Name: r.Name, Type: r.Type, Definition: r.Definition})
	}
	return out, nil
}

func strDeref(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
