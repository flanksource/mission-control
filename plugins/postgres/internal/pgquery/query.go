package pgquery

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

type Result struct {
	ResultSets   []ResultSet      `json:"resultSets,omitempty"`
	Columns      []string         `json:"columns"`
	ColumnTypes  []ColumnType     `json:"columnTypes,omitempty"`
	Rows         []map[string]any `json:"rows"`
	RowCount     int              `json:"rowCount"`
	RowsAffected int64            `json:"rowsAffected,omitempty"`
	DurationMS   int64            `json:"durationMs"`
	IsSelect     bool             `json:"isSelect"`
	Statement    string           `json:"statement"`
	Database     string           `json:"database,omitempty"`
}

type ResultSet struct {
	Columns      []string         `json:"columns,omitempty"`
	ColumnTypes  []ColumnType     `json:"columnTypes,omitempty"`
	Rows         []map[string]any `json:"rows,omitempty"`
	RowCount     int              `json:"rowCount"`
	RowsAffected int64            `json:"rowsAffected,omitempty"`
	HasRows      bool             `json:"hasRows"`
}

type ColumnType struct {
	Name     string `json:"name"`
	Type     string `json:"type,omitempty"`
	Nullable bool   `json:"nullable,omitempty"`
}

type Options struct {
	Database string
	RowLimit int
}

func Execute(ctx context.Context, db *gorm.DB, statement string, opts Options) (*Result, error) {
	statement = strings.TrimSpace(statement)
	if statement == "" {
		return nil, errors.New("statement is required")
	}
	res := &Result{Statement: statement, Database: opts.Database}
	start := time.Now()
	defer func() { res.DurationMS = time.Since(start).Milliseconds() }()

	conn, release, err := pinnedConn(ctx, db)
	if err != nil {
		return res, err
	}
	defer release()

	if rowReturning(statement) {
		rows, err := conn.QueryContext(ctx, statement)
		if err != nil {
			return res, err
		}
		defer rows.Close()
		if err := readRows(rows, opts.RowLimit, res); err != nil {
			return res, err
		}
		return res, nil
	}

	r, err := conn.ExecContext(ctx, statement)
	if err != nil {
		return res, err
	}
	if n, err := r.RowsAffected(); err == nil {
		res.RowsAffected = n
		res.ResultSets = []ResultSet{{RowsAffected: n}}
	}
	return res, nil
}

func Explain(ctx context.Context, db *gorm.DB, statement string) (string, error) {
	statement = strings.TrimSpace(statement)
	if statement == "" {
		return "", errors.New("statement is required")
	}
	conn, release, err := pinnedConn(ctx, db)
	if err != nil {
		return "", err
	}
	defer release()

	rows, err := conn.QueryContext(ctx, "EXPLAIN (FORMAT JSON) "+statement)
	if err != nil {
		return "", fmt.Errorf("explain: %w", err)
	}
	defer rows.Close()

	var parts []string
	for rows.Next() {
		var v any
		if err := rows.Scan(&v); err != nil {
			return "", err
		}
		parts = append(parts, stringify(v))
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return strings.Join(parts, "\n"), nil
}

func pinnedConn(ctx context.Context, db *gorm.DB) (*sql.Conn, func(), error) {
	sqlDB, err := db.DB()
	if err != nil {
		return nil, nil, fmt.Errorf("unwrap db: %w", err)
	}
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("acquire connection: %w", err)
	}
	return conn, func() { _ = conn.Close() }, nil
}

func readRows(rows *sql.Rows, rowLimit int, res *Result) error {
	set, err := readOneResultSet(rows, rowLimit)
	if err != nil {
		return err
	}
	res.ResultSets = []ResultSet{set}
	res.Columns = set.Columns
	res.ColumnTypes = set.ColumnTypes
	res.Rows = set.Rows
	res.RowCount = set.RowCount
	res.IsSelect = true
	return rows.Err()
}

func readOneResultSet(rows *sql.Rows, rowLimit int) (ResultSet, error) {
	var set ResultSet
	cols, err := rows.Columns()
	if err != nil {
		return set, err
	}
	set.Columns = cols
	set.HasRows = true
	if cts, err := rows.ColumnTypes(); err == nil {
		set.ColumnTypes = make([]ColumnType, len(cts))
		for i, ct := range cts {
			nullable, _ := ct.Nullable()
			set.ColumnTypes[i] = ColumnType{Name: ct.Name(), Type: ct.DatabaseTypeName(), Nullable: nullable}
		}
	}
	for rows.Next() {
		if rowLimit > 0 && len(set.Rows) >= rowLimit {
			break
		}
		row, err := scanRow(rows, cols, set.ColumnTypes)
		if err != nil {
			return set, err
		}
		set.Rows = append(set.Rows, row)
	}
	set.RowCount = len(set.Rows)
	return set, nil
}

func scanRow(rows *sql.Rows, cols []string, types []ColumnType) (map[string]any, error) {
	values := make([]any, len(cols))
	scanTargets := make([]any, len(cols))
	for i := range values {
		scanTargets[i] = &values[i]
	}
	if err := rows.Scan(scanTargets...); err != nil {
		return nil, err
	}
	out := make(map[string]any, len(cols))
	for i, col := range cols {
		out[col] = normalizeValue(values[i], typeAt(types, i))
	}
	return out, nil
}

func rowReturning(statement string) bool {
	s := strings.TrimSpace(strings.TrimLeft(statement, "("))
	if s == "" {
		return false
	}
	first := strings.ToLower(strings.Fields(s)[0])
	switch first {
	case "select", "show", "with", "values", "explain", "table":
		return true
	case "insert", "update", "delete":
		return strings.Contains(strings.ToLower(s), " returning ")
	default:
		return false
	}
}

func normalizeValue(v any, typ string) any {
	switch x := v.(type) {
	case nil:
		return nil
	case []byte:
		return string(x)
	case time.Time:
		return x.Format(time.RFC3339Nano)
	default:
		return x
	}
}

func typeAt(types []ColumnType, i int) string {
	if i < len(types) {
		return types[i].Type
	}
	return ""
}

func stringify(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case []byte:
		return string(x)
	case string:
		return x
	default:
		return fmt.Sprint(x)
	}
}
