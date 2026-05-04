// Package sqlquery executes ad-hoc SQL statements and returns generic
// rows + columns suitable for the console UI's table renderer.
package sqlquery

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// Result is the response shape for the `query` operation.
//
// SQL Server batches can produce multiple result sets — e.g. an INSERT
// followed by a SELECT, or a stored procedure with several SELECTs. The
// FE renders ResultSets when present; the top-level Columns/Rows fields
// are populated from the last non-empty set so older callers keep working.
type Result struct {
	// Per-batch result sets, in execution order. One entry per "statement
	// that produced a row set OR reported rows-affected".
	ResultSets []ResultSet `json:"resultSets,omitempty"`

	// Convenience fields mirroring the last non-empty result set. Kept for
	// backward-compat with the original single-set Console renderer.
	Columns      []string         `json:"columns"`
	ColumnTypes  []ColumnType     `json:"columnTypes,omitempty"`
	Rows         []map[string]any `json:"rows"`
	RowCount     int              `json:"rowCount"`
	RowsAffected int64            `json:"rowsAffected,omitempty"`

	DurationMS int64  `json:"durationMs"`
	IsSelect   bool   `json:"isSelect"`
	Statement  string `json:"statement"`
	Database   string `json:"database,omitempty"`
}

type ResultSet struct {
	Columns      []string         `json:"columns,omitempty"`
	ColumnTypes  []ColumnType     `json:"columnTypes,omitempty"`
	Rows         []map[string]any `json:"rows,omitempty"`
	RowCount     int              `json:"rowCount"`
	RowsAffected int64            `json:"rowsAffected,omitempty"`
	// HasRows is true when the batch returned a row set (even an empty
	// one) — distinguishes "SELECT returning 0 rows" from "INSERT".
	HasRows bool `json:"hasRows"`
}

type ColumnType struct {
	Name     string `json:"name"`
	Type     string `json:"type,omitempty"`
	Nullable bool   `json:"nullable,omitempty"`
}

// Options controls Execute. Database, when non-empty, runs USE <db> before
// the statement. RowLimit caps the rows returned (defensive — the console
// can ask for a million rows otherwise). Zero means no cap.
type Options struct {
	Database string
	RowLimit int
}

// Execute runs the given SQL against db with options. The batch may
// contain multiple statements separated by semicolons; every result set
// the driver surfaces via NextResultSet is captured into ResultSets.
//
// The top-level Columns/Rows/RowsAffected mirror the last non-empty
// set so legacy single-set callers keep rendering. IsSelect is true when
// at least one set returned rows.
//
// When opts.Database is set, USE is issued on a pinned single-connection
// handle so the database switch is guaranteed to apply to the immediately
// following statement (gorm's pool may otherwise route the two calls to
// different physical connections).
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

	if opts.Database != "" {
		if _, err := conn.ExecContext(ctx, "USE "+quoteDB(opts.Database)); err != nil {
			return res, fmt.Errorf("use %s: %w", opts.Database, err)
		}
	}

	rows, err := conn.QueryContext(ctx, statement)
	if err != nil {
		return res, err
	}
	defer rows.Close()

	if err := readAllResultSets(rows, opts.RowLimit, res); err != nil {
		return res, err
	}
	return res, nil
}

// pinnedConn returns a single *sql.Conn from gorm's underlying pool,
// plus a release func that returns it to the pool. Callers MUST call
// release exactly once (deferred). The conn is what guarantees that
// USE + the statement land on the same physical connection.
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

// readAllResultSets walks every set the driver surfaces. SQL Server returns
// one set per row-producing statement in the batch and uses NextResultSet
// to advance between them. Sets that returned no row metadata (e.g. an
// INSERT in the middle of a batch) appear as RowsAffected-only entries.
func readAllResultSets(rows *sql.Rows, rowLimit int, res *Result) error {
	for {
		set, err := readOneResultSet(rows, rowLimit)
		if err != nil {
			return err
		}
		// Skip purely empty no-op sets the driver may emit between batches.
		if set.HasRows || set.RowsAffected != 0 || len(set.Columns) > 0 {
			res.ResultSets = append(res.ResultSets, set)
		}
		if !rows.NextResultSet() {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	// Promote the last row-bearing set to the top-level fields for
	// backward-compat. If no set produced rows, surface the last one's
	// RowsAffected (the only meaningful signal for INSERT/UPDATE/DELETE).
	for i := len(res.ResultSets) - 1; i >= 0; i-- {
		s := res.ResultSets[i]
		if s.HasRows {
			res.Columns = s.Columns
			res.ColumnTypes = s.ColumnTypes
			res.Rows = s.Rows
			res.RowCount = s.RowCount
			res.IsSelect = true
			break
		}
	}
	if !res.IsSelect && len(res.ResultSets) > 0 {
		res.RowsAffected = res.ResultSets[len(res.ResultSets)-1].RowsAffected
	}
	return nil
}

func readOneResultSet(rows *sql.Rows, rowLimit int) (ResultSet, error) {
	var set ResultSet
	cols, err := rows.Columns()
	if err != nil {
		// No columns on this set — it's a non-row-returning statement
		// (INSERT/UPDATE/DELETE). The driver doesn't expose RowsAffected
		// on *sql.Rows, so the count stays zero. (For a single INSERT/etc.,
		// callers already get RowsAffected via the Exec path; this branch
		// only matters for mid-batch DML statements.)
		return set, nil
	}
	set.Columns = cols
	set.HasRows = true
	if cts, err := rows.ColumnTypes(); err == nil {
		set.ColumnTypes = make([]ColumnType, len(cts))
		for i, ct := range cts {
			ctype := ct.DatabaseTypeName()
			nullable, _ := ct.Nullable()
			set.ColumnTypes[i] = ColumnType{Name: ct.Name(), Type: ctype, Nullable: nullable}
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

// Explain runs SET SHOWPLAN_{XML,TEXT} ON, executes the statement (which
// returns the plan instead of rows), and disables it. Each SHOWPLAN
// command must live in its own batch and ALL of them — USE, SET ON,
// statement, SET OFF — must land on the same connection.
func Explain(ctx context.Context, db *gorm.DB, statement, database, format string) (string, error) {
	statement = strings.TrimSpace(statement)
	if statement == "" {
		return "", errors.New("statement is required")
	}
	if format == "" {
		format = "xml"
	}
	if format != "xml" && format != "text" {
		return "", fmt.Errorf("format must be xml or text")
	}
	conn, release, err := pinnedConn(ctx, db)
	if err != nil {
		return "", err
	}
	defer release()

	if database != "" {
		if _, err := conn.ExecContext(ctx, "USE "+quoteDB(database)); err != nil {
			return "", fmt.Errorf("use %s: %w", database, err)
		}
	}
	on := "SET SHOWPLAN_XML ON"
	off := "SET SHOWPLAN_XML OFF"
	if format == "text" {
		on = "SET SHOWPLAN_TEXT ON"
		off = "SET SHOWPLAN_TEXT OFF"
	}
	if _, err := conn.ExecContext(ctx, on); err != nil {
		return "", fmt.Errorf("enable showplan: %w", err)
	}
	defer func() { _, _ = conn.ExecContext(ctx, off) }()
	var plan string
	if err := conn.QueryRowContext(ctx, statement).Scan(&plan); err != nil {
		return "", fmt.Errorf("run plan: %w", err)
	}
	return plan, nil
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
		var typeName string
		if i < len(types) {
			typeName = types[i].Type
		}
		out[col] = normalizeValue(values[i], typeName)
	}
	return out, nil
}

// normalizeValue turns driver-specific scan values into JSON-friendly types:
//   - UNIQUEIDENTIFIER 16-byte payloads are reformatted as canonical
//     hyphenated GUIDs (the MSSQL driver only does this when scanning
//     into mssql.UniqueIdentifier; scanning into `any` lands as raw bytes).
//   - other []byte → string (text/varchar columns)
//   - time.Time → RFC3339Nano (UTC)
func normalizeValue(v any, columnType string) any {
	switch x := v.(type) {
	case []byte:
		if isUniqueIdentifier(columnType) && len(x) == 16 {
			return formatGUID(x)
		}
		return string(x)
	case time.Time:
		return x.UTC().Format(time.RFC3339Nano)
	default:
		return v
	}
}

func isUniqueIdentifier(columnType string) bool {
	if columnType == "" {
		return false
	}
	return strings.EqualFold(columnType, "UNIQUEIDENTIFIER")
}

// formatGUID reformats SQL Server's wire layout for a UNIQUEIDENTIFIER
// (little-endian time-low/mid/hi, then big-endian clock-seq + node) into
// the canonical hyphenated form, e.g.
// "01234567-89AB-CDEF-0123-456789ABCDEF". Mirrors mssql.UniqueIdentifier.Scan.
func formatGUID(b []byte) string {
	g := make([]byte, 16)
	copy(g, b)
	reverse(g[0:4])
	reverse(g[4:6])
	reverse(g[6:8])
	const hex = "0123456789abcdef"
	out := make([]byte, 36)
	groups := []struct{ start, end int }{{0, 4}, {4, 6}, {6, 8}, {8, 10}, {10, 16}}
	pos := 0
	for gi, grp := range groups {
		for i := grp.start; i < grp.end; i++ {
			out[pos] = hex[g[i]>>4]
			out[pos+1] = hex[g[i]&0x0f]
			pos += 2
		}
		if gi != len(groups)-1 {
			out[pos] = '-'
			pos++
		}
	}
	return string(out)
}

func reverse(b []byte) {
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
}

func quoteDB(name string) string {
	return "[" + strings.ReplaceAll(name, "]", "]]") + "]"
}
