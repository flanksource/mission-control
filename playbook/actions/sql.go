package actions

import (
	"database/sql"
	"fmt"

	"github.com/flanksource/gomplate/v3"
	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
)

type SQLResult struct {
	Rows  []map[string]interface{} `json:"rows,omitempty"`
	Count int                      `json:"count,omitempty"`
}

type SQL struct{}

// Run - Check every entry from config according to Checker interface
// Returns check result and metrics
func (c *SQL) Run(ctx api.Context, action v1.SQLAction, env TemplateEnv) (*SQLResult, error) {
	templated, err := gomplate.RunTemplate(env.AsMap(), gomplate.Template{Template: action.Query})
	if err != nil {
		return nil, err
	}
	action.Query = templated

	if action.Connection != "" {
		connection, err := ctx.HydrateConnection(action.Connection)
		if err != nil {
			return nil, fmt.Errorf("error getting connection: %w", err)
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
	result := SQLResult{}
	if err != nil || rows.Err() != nil {
		return nil, fmt.Errorf("failed to query db: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	for rows.Next() {
		var rowValues = make([]interface{}, len(columns))
		for i := range rowValues {
			var s sql.NullString
			rowValues[i] = &s
		}
		if err := rows.Scan(rowValues...); err != nil {
			return nil, err
		}

		var row = make(map[string]interface{})
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
