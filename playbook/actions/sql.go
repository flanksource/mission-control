package actions

import (
	"database/sql"
	"fmt"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	pkgConnection "github.com/flanksource/duty/connection"
	"github.com/flanksource/duty/context"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

type SQLResult struct {
	Query   string           `json:"query,omitempty"`
	Rows    []map[string]any `json:"rows,omitempty"`
	Count   int              `json:"count"`
	Columns []string         `json:"columns,omitempty"` // Used for maintaining order in UI
}

type SQL struct{}

// Run - Check every entry from config according to Checker interface
// Returns check result and metrics
func (c *SQL) Run(ctx context.Context, action v1.SQLAction) (*SQLResult, error) {
	if action.Connection != "" {
		connection, err := pkgConnection.Get(ctx, action.Connection)
		if err != nil {
			return nil, fmt.Errorf("error getting connection: %w", err)
		} else if connection == nil {
			return nil, fmt.Errorf("connection(%s) was not found", action.Connection)
		}

		action.URL = connection.URL
	}

	details, err := querySQL(action.Driver, action.URL, action.Query)
	if err != nil {
		return nil, fmt.Errorf("error querying db: %w", err)
	}

	return details, nil
}

// querySQL connects to a db using the specified `driver` and `connectionstring`
// Performs the test query given in `query`.
// Gives the single row test query result as result.
func querySQL(driver string, connection string, query string) (*SQLResult, error) {
	db, err := sql.Open(driver, connection)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to db: %w", err)
	}
	defer db.Close()

	rows, err := db.Query(query)
	result := SQLResult{Query: query}
	if err != nil || rows.Err() != nil {
		return nil, fmt.Errorf("failed to query db: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}
	result.Columns = columns

	for rows.Next() {
		var rowValues = make([]any, len(columns))
		for i := range rowValues {
			var s sql.NullString
			rowValues[i] = &s
		}
		if err := rows.Scan(rowValues...); err != nil {
			return nil, err
		}

		var row = make(map[string]any)
		for i, val := range rowValues {
			v := *val.(*sql.NullString)
			if v.Valid {
				row[columns[i]] = v.String
			} else {
				row[columns[i]] = nil
			}
		}

		result.Rows = append(result.Rows, row)
	}

	result.Count = len(result.Rows)
	return &result, nil
}

func (r SQLResult) String() string   { return r.table().String() }
func (r SQLResult) ANSI() string     { return "\n" + r.table().ANSI() }
func (r SQLResult) HTML() string     { return r.table().HTML() }
func (r SQLResult) Markdown() string { return "\n" + r.table().Markdown() }

func (r SQLResult) table() api.TextTable {
	headers := make([]api.Textable, len(r.Columns))
	for i, col := range r.Columns {
		headers[i] = clicky.Text(col, "font-bold")
	}

	rows := make([]api.TableRow, len(r.Rows))
	for i, row := range r.Rows {
		tr := make(api.TableRow)
		for _, col := range r.Columns {
			val := "NULL"
			if v, exists := row[col]; exists && v != nil {
				val = fmt.Sprint(v)
			}
			tr[col] = api.TypedValue{Textable: clicky.Text(val, "")}
		}
		rows[i] = tr
	}

	return api.TextTable{
		Headers:    headers,
		Rows:       rows,
		FieldNames: r.Columns,
	}
}
