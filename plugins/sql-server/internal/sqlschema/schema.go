// Package sqlschema introspects an MSSQL database via INFORMATION_SCHEMA
// and returns a {tables: [{schema, name, columns: [{name, dataType}]}]}
// payload. Powers the Console tab's Monaco autocomplete provider.
package sqlschema

import (
	"context"
	"database/sql"
	"fmt"
	"sort"

	"gorm.io/gorm"
)

type Response struct {
	Tables   []Table `json:"tables"`
	Database string  `json:"database,omitempty"`
}

type Table struct {
	Name    string   `json:"name"`
	Schema  string   `json:"schema"`
	Columns []Column `json:"columns"`
}

type Column struct {
	Name     string `json:"name"`
	DataType string `json:"dataType"`
}

// Options narrows the scan. When Database is set, the queries explicitly
// reference INFORMATION_SCHEMA on that database — otherwise the connection's
// default database is used. MaxColumnsPerTable caps oversized tables to keep
// the autocomplete payload manageable; zero means no cap.
type Options struct {
	Database           string
	MaxColumnsPerTable int
}

type tableRow struct {
	Schema string `gorm:"column:TABLE_SCHEMA"`
	Name   string `gorm:"column:TABLE_NAME"`
}

type columnRow struct {
	Schema   string `gorm:"column:TABLE_SCHEMA"`
	Table    string `gorm:"column:TABLE_NAME"`
	Name     string `gorm:"column:COLUMN_NAME"`
	DataType string `gorm:"column:DATA_TYPE"`
}

// Introspect runs two INFORMATION_SCHEMA queries (tables, columns) and
// joins them in-process. Splitting the query avoids any single oversized
// scan.
//
// All driver round-trips happen on a single pinned connection so the USE
// (when a Database is requested) is guaranteed to apply to both the
// tables and columns queries that follow.
func Introspect(ctx context.Context, db *gorm.DB, opts Options) (*Response, error) {
	if db == nil {
		return nil, fmt.Errorf("nil db")
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("unwrap db: %w", err)
	}
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Close()

	if opts.Database != "" {
		if _, err := conn.ExecContext(ctx, "USE "+quoteDB(opts.Database)); err != nil {
			return nil, fmt.Errorf("use %s: %w", opts.Database, err)
		}
	}
	tables, err := scanTableRows(ctx, conn,
		`SELECT TABLE_SCHEMA, TABLE_NAME
         FROM INFORMATION_SCHEMA.TABLES
         WHERE TABLE_TYPE = 'BASE TABLE'
         ORDER BY TABLE_SCHEMA, TABLE_NAME`,
	)
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}
	columns, err := scanColumnRows(ctx, conn,
		`SELECT TABLE_SCHEMA, TABLE_NAME, COLUMN_NAME, DATA_TYPE
         FROM INFORMATION_SCHEMA.COLUMNS
         ORDER BY TABLE_SCHEMA, TABLE_NAME, ORDINAL_POSITION`,
	)
	if err != nil {
		return nil, fmt.Errorf("list columns: %w", err)
	}

	type key struct{ schema, name string }
	byTable := make(map[key][]Column, len(tables))
	for _, c := range columns {
		k := key{schema: c.Schema, name: c.Table}
		if opts.MaxColumnsPerTable > 0 && len(byTable[k]) >= opts.MaxColumnsPerTable {
			continue
		}
		byTable[k] = append(byTable[k], Column{Name: c.Name, DataType: c.DataType})
	}

	out := make([]Table, 0, len(tables))
	for _, t := range tables {
		out = append(out, Table{
			Name:    t.Name,
			Schema:  t.Schema,
			Columns: byTable[key{schema: t.Schema, name: t.Name}],
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Schema != out[j].Schema {
			return out[i].Schema < out[j].Schema
		}
		return out[i].Name < out[j].Name
	})
	return &Response{Tables: out, Database: opts.Database}, nil
}

func scanTableRows(ctx context.Context, conn *sql.Conn, query string) ([]tableRow, error) {
	rows, err := conn.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []tableRow
	for rows.Next() {
		var r tableRow
		if err := rows.Scan(&r.Schema, &r.Name); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func scanColumnRows(ctx context.Context, conn *sql.Conn, query string) ([]columnRow, error) {
	rows, err := conn.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []columnRow
	for rows.Next() {
		var r columnRow
		if err := rows.Scan(&r.Schema, &r.Table, &r.Name, &r.DataType); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func quoteDB(name string) string {
	out := make([]byte, 0, len(name)+2)
	out = append(out, '[')
	for i := 0; i < len(name); i++ {
		if name[i] == ']' {
			out = append(out, ']', ']')
			continue
		}
		out = append(out, name[i])
	}
	out = append(out, ']')
	return string(out)
}
