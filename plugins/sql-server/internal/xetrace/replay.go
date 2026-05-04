package xetrace

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
)

// RPCCall is the structured form of an sp_executesql / sp_prepexec
// invocation. It is what ParseRPC returns and what Replay() consumes to
// rebuild a parameterized query with typed bindings — as opposed to
// UnwrapRPC, which only produces a display string with inlined literals.
type RPCCall struct {
	// Template is the inner SQL with @P0/@P1/... placeholders preserved.
	Template string
	// ParamDecl is the SQL Server parameter declaration string, e.g.
	// "@P0 int, @P1 nvarchar(10)". Empty when the RPC shipped without
	// declared types.
	ParamDecl string
	// Values are the raw tokens (N'…', 42, 0x1A2B, NULL, …) in declaration
	// order, exactly as they came out of splitTopLevelArgs.
	Values []string
}

// ParseRPC parses a raw trace statement into an RPCCall if it matches one
// of the supported RPC shapes. Returns ok=false for plain statements and
// for positional CALL syntax (which the trace layer doesn't expose values
// for anyway). Delegates tokenization to the same helpers as UnwrapRPC.
func ParseRPC(raw string) (*RPCCall, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, false
	}
	if stripped, didStrip := stripPrepexecScaffold(trimmed); didStrip {
		return parsePrepexecStructured(stripped)
	}
	return parseExecuteSQLStructured(trimmed)
}

func parsePrepexecStructured(body string) (*RPCCall, bool) {
	args, ok := splitTopLevelArgs(body)
	if !ok || len(args) < 2 {
		return nil, false
	}
	decl := ""
	if !strings.EqualFold(strings.TrimSpace(args[0]), "NULL") {
		s, stringOK := asStringLiteral(args[0])
		if !stringOK {
			return nil, false
		}
		decl = s
	}
	template, ok := asStringLiteral(args[1])
	if !ok {
		return nil, false
	}
	return &RPCCall{Template: template, ParamDecl: decl, Values: args[2:]}, true
}

func parseExecuteSQLStructured(s string) (*RPCCall, bool) {
	loc := executeSQLRe.FindStringIndex(s)
	if loc == nil {
		return nil, false
	}
	body := strings.TrimSpace(s[loc[1]:])
	args, ok := splitTopLevelArgs(body)
	if !ok || len(args) < 1 {
		return nil, false
	}
	template, ok := asStringLiteral(args[0])
	if !ok {
		return nil, false
	}
	decl := ""
	var values []string
	if len(args) >= 2 {
		if d, declOK := asStringLiteral(args[1]); declOK {
			decl = d
			values = args[2:]
		} else {
			values = args[1:]
		}
	}
	return &RPCCall{Template: template, ParamDecl: decl, Values: values}, true
}

// ToGormQuery returns the replayable form of the call: a SQL string with
// ordered `?` placeholders (gorm+sqlserver rewrites these to @p1/@p2 at
// bind time) and the positional args, type-coerced when a declaration
// provides enough info. When decl is missing, values are passed as
// strings and the driver does its best.
func (c *RPCCall) ToGormQuery() (string, []any, []coerceWarning) {
	placeholders := parseParamDecl(c.ParamDecl)
	// If the decl is empty but the template has @PN refs, synthesize untyped
	// placeholders in declaration order so the replay still tries to bind.
	if len(placeholders) == 0 {
		names := paramNameRe.FindAllString(c.Template, -1)
		names = dedupePreserveOrder(names)
		for _, n := range names {
			placeholders = append(placeholders, paramInfo{Name: n, SQLType: ""})
		}
	}

	args := make([]any, 0, len(placeholders))
	var warnings []coerceWarning
	for i, p := range placeholders {
		if i >= len(c.Values) {
			args = append(args, nil)
			continue
		}
		val, warn := coerceValue(c.Values[i], p.SQLType)
		args = append(args, val)
		if warn != "" {
			warnings = append(warnings, coerceWarning{
				ParamName: p.Name,
				RawToken:  c.Values[i],
				SQLType:   p.SQLType,
				Message:   warn,
			})
		}
	}

	// Rewrite @PN in the template to ordered `?` placeholders. We must do
	// the replacement in declaration order so positional args line up with
	// the first textual appearance of each name.
	sqlText := c.Template
	for _, p := range placeholders {
		re := regexp.MustCompile(regexp.QuoteMeta(p.Name) + `\b`)
		sqlText = re.ReplaceAllLiteralString(sqlText, "?")
	}
	return strings.TrimSpace(sqlText), args, warnings
}

// paramInfo is one entry from a parsed SQL Server param declaration.
type paramInfo struct {
	Name    string // e.g. "@P0"
	SQLType string // e.g. "int", "nvarchar", "datetime2"
}

// coerceWarning captures a non-fatal problem encountered while mapping a
// raw trace token to a Go value. Surfaced on ReplayResult so callers can
// see when type fidelity was compromised.
type coerceWarning struct {
	ParamName string `json:"param"`
	RawToken  string `json:"raw"`
	SQLType   string `json:"sql_type,omitempty"`
	Message   string `json:"message"`
}

// paramDeclEntryRe matches one `@PN type[(args)]` entry inside a decl
// string. Type is the first identifier token (letters then optional
// trailing digits, e.g. `datetime2`); args and modifiers after it
// (e.g. `(10)`, `(max)`, `NOT NULL`) are ignored by the coercer.
var paramDeclEntryRe = regexp.MustCompile(`(?i)(@P\d+)\s+([a-z_]+[a-z0-9_]*)`)

// parseParamDecl splits a SQL Server parameter declaration into its
// @PN + type entries. Tolerant: missing types, extra whitespace, trailing
// modifiers, and length specifiers all parse.
func parseParamDecl(decl string) []paramInfo {
	matches := paramDeclEntryRe.FindAllStringSubmatch(decl, -1)
	out := make([]paramInfo, 0, len(matches))
	for _, m := range matches {
		out = append(out, paramInfo{Name: m[1], SQLType: strings.ToLower(m[2])})
	}
	return out
}

// coerceValue maps a raw tokenized arg from a trace (`N'foo'`, `42`,
// `0x1A2B`, `NULL`) into a Go value whose dynamic type matches sqlType.
// Returns a non-empty warning string when the mapping fell back — for
// example when the type is unknown, or the token shape does not match the
// declared type so we had to pass a string through.
func coerceValue(raw, sqlType string) (any, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "NULL") {
		return nil, ""
	}
	// String literal: keep it as a string; the type tells us whether it
	// should round-trip through time.Parse for datetime columns.
	if unquoted, ok := asStringLiteral(raw); ok {
		return coerceStringValue(unquoted, sqlType)
	}
	// Hex binary literal.
	if strings.HasPrefix(raw, "0x") || strings.HasPrefix(raw, "0X") {
		if b, err := hex.DecodeString(raw[2:]); err == nil {
			return b, ""
		}
		return raw, fmt.Sprintf("could not decode hex literal %q", raw)
	}
	// Numeric literal.
	switch sqlType {
	case "int", "bigint", "smallint", "tinyint":
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
			return n, ""
		}
		return raw, fmt.Sprintf("could not parse %q as %s", raw, sqlType)
	case "bit":
		if raw == "0" {
			return false, ""
		}
		if raw == "1" {
			return true, ""
		}
		return raw, fmt.Sprintf("could not parse %q as bit", raw)
	case "float", "real", "numeric", "decimal", "money", "smallmoney":
		if f, err := strconv.ParseFloat(raw, 64); err == nil {
			return f, ""
		}
		return raw, fmt.Sprintf("could not parse %q as %s", raw, sqlType)
	case "":
		// No type hint. Try int then float; fall back to string.
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
			return n, ""
		}
		if f, err := strconv.ParseFloat(raw, 64); err == nil {
			return f, ""
		}
		return raw, ""
	default:
		return raw, fmt.Sprintf("unknown numeric type %q", sqlType)
	}
}

// coerceStringValue handles the tail of coerceValue for string-literal
// inputs. Datetime columns parse into time.Time; uniqueidentifier stays as
// a string (the driver accepts it); everything else passes through as a
// string.
func coerceStringValue(val, sqlType string) (any, string) {
	switch sqlType {
	case "datetime", "datetime2", "smalldatetime", "date":
		for _, layout := range []string{
			"2006-01-02 15:04:05.9999999",
			"2006-01-02 15:04:05",
			"2006-01-02T15:04:05.9999999",
			"2006-01-02T15:04:05",
			"2006-01-02",
		} {
			if t, err := time.Parse(layout, val); err == nil {
				return t, ""
			}
		}
		return val, fmt.Sprintf("could not parse datetime %q", val)
	case "time":
		for _, layout := range []string{"15:04:05.9999999", "15:04:05"} {
			if t, err := time.Parse(layout, val); err == nil {
				return t, ""
			}
		}
		return val, fmt.Sprintf("could not parse time %q", val)
	case "uniqueidentifier", "nvarchar", "varchar", "nchar", "char", "text", "ntext", "":
		return val, ""
	default:
		return val, fmt.Sprintf("unknown string type %q", sqlType)
	}
}

// ReplayResult is one entry in TraceResult.Replays: the outcome of
// replaying a single captured event. Serializable as JSON so --format
// json surfaces it alongside the raw trace data.
type ReplayResult struct {
	// EventKey links back to Event.Key() for correlation with the
	// original trace row.
	EventKey string `json:"event_key"`
	// OriginalStatement is the raw statement text as it came out of the
	// trace, kept for display so users can compare what was captured with
	// what was actually replayed.
	OriginalStatement string `json:"original_statement"`
	// Eligible indicates whether the replay actually ran. When false,
	// SkipReason explains why (non-SELECT, INTO/EXEC present, error
	// event, etc.).
	Eligible   bool   `json:"eligible"`
	SkipReason string `json:"skip_reason,omitempty"`
	// SQL and Args are the parameterized form passed to the driver —
	// useful for users who want to re-run the query manually.
	SQL  string `json:"sql,omitempty"`
	Args []any  `json:"args,omitempty"`
	// RowCount is the exact number of rows the replay returned. When
	// RowCountTruncated is true the replay hit the scan cap and the
	// count is a lower bound.
	RowCount          int  `json:"row_count"`
	RowCountTruncated bool `json:"row_count_truncated,omitempty"`
	// FirstRows holds up to 3 column-name-keyed maps for display.
	FirstRows []map[string]any `json:"first_rows,omitempty"`
	// Warnings from type coercion (e.g. unknown SQL type, unparseable
	// datetime) so the user can see where fidelity may have been lost.
	Warnings []coerceWarning `json:"warnings,omitempty"`
	// Error is the driver or scan error, if any. Non-empty means the
	// replay ran but failed partway; Eligible is still true in that case.
	Error string `json:"error,omitempty"`
	// Duration is the wall time spent on this replay.
	Duration time.Duration `json:"duration"`
}

// replayRowCap bounds row scans so a "SELECT * FROM AsActivity" replay
// doesn't melt the DB. Count-past-cap increments a truncation flag.
const replayRowCap = 10000

// replayPerQueryTimeout is the per-statement deadline. Intentionally
// small: replay is a diagnostic, not a backfill job.
const replayPerQueryTimeout = 10 * time.Second

// isReplayable reports whether an event is eligible for replay. The
// rules are intentionally conservative: only SELECT statements that
// don't contain INTO or EXEC tokens. Anything else is skipped with a
// reason string the caller surfaces in ReplayResult.
func isReplayable(e Event, template string) (bool, string) {
	if e.Name == EventErrorReported {
		return false, "error event"
	}
	if template == "" {
		return false, "empty statement"
	}
	upper := strings.ToUpper(collapseWhitespace(strings.TrimSpace(template)))
	if !strings.HasPrefix(upper, "SELECT") {
		return false, "not a SELECT"
	}
	// Word-boundary match for INTO and EXEC/EXECUTE. Use explicit \b
	// rather than substring so "INTRODUCTION_DATE" or "EXECUTIONNO"
	// column names don't trip the filter.
	if intoExecRe.MatchString(upper) {
		return false, "contains INTO or EXEC"
	}
	return true, ""
}

var intoExecRe = regexp.MustCompile(`\b(INTO|EXEC(UTE)?)\b`)

// Replay walks the trace events, picks the ones that are safe to re-run,
// and executes each against db. The returned slice has one entry per
// input event (including skipped ones) so the caller can display both
// the replay results and why some were not replayed.
func Replay(ctx context.Context, db *gorm.DB, events []Event) []ReplayResult {
	out := make([]ReplayResult, 0, len(events))
	for _, e := range events {
		out = append(out, ReplayOne(ctx, db, e))
	}
	return out
}

// ReplayOne replays a single event and returns its result. Exported so
// the CLI drain loop can run replay inline immediately after streaming
// the original trace line, keeping replay output interleaved with the
// rest of the live output.
func ReplayOne(ctx context.Context, db *gorm.DB, e Event) ReplayResult {
	res := ReplayResult{
		EventKey:          e.Key(),
		OriginalStatement: e.Statement,
	}

	// Try the RPC parse path first; fall back to the raw statement when
	// the event is a plain sql_statement_completed.
	var template string
	var sqlText string
	var args []any
	var warnings []coerceWarning
	if call, ok := ParseRPC(e.Statement); ok {
		template = call.Template
		sqlText, args, warnings = call.ToGormQuery()
	} else {
		template = e.Statement
		sqlText = e.Statement
	}

	eligible, reason := isReplayable(e, template)
	if !eligible {
		res.Eligible = false
		res.SkipReason = reason
		return res
	}

	res.Eligible = true
	res.SQL = sqlText
	res.Args = args
	res.Warnings = warnings

	queryCtx, cancel := context.WithTimeout(ctx, replayPerQueryTimeout)
	defer cancel()
	start := time.Now()
	rows, err := db.WithContext(queryCtx).Raw(sqlText, args...).Rows()
	if err != nil {
		res.Error = err.Error()
		res.Duration = time.Since(start)
		return res
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		res.Error = fmt.Sprintf("columns: %v", err)
		res.Duration = time.Since(start)
		return res
	}
	colTypes := make([]string, len(columns))
	if types, typErr := rows.ColumnTypes(); typErr == nil {
		for i, t := range types {
			colTypes[i] = t.DatabaseTypeName()
		}
	}

	firstRows, rowCount, truncated, scanErr := scanRowsCapped(rows, columns, colTypes, replayRowCap)
	res.FirstRows = firstRows
	res.RowCount = rowCount
	res.RowCountTruncated = truncated
	if scanErr != nil {
		res.Error = scanErr.Error()
	}
	res.Duration = time.Since(start)
	return res
}

// scanRowsCapped iterates rows, keeps the first 3 as maps keyed by column
// name, and returns the exact row count or the cap when truncated.
// colTypes parallels columns and holds the driver-reported database type
// name (e.g. "UNIQUEIDENTIFIER", "VARBINARY", "NVARCHAR") so
// formatScanValue can render bytes correctly — a missing entry (empty
// string) falls through to the UTF-8-or-hex fallback branch.
func scanRowsCapped(rows *sql.Rows, columns, colTypes []string, cap int) ([]map[string]any, int, bool, error) {
	var first []map[string]any
	count := 0
	for rows.Next() {
		if count >= cap {
			// Consume but don't scan once we hit the cap — still need
			// rows.Next() to advance so the driver can unblock.
			count++
			continue
		}
		vals := make([]any, len(columns))
		ptrs := make([]any, len(columns))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return first, count, false, err
		}
		if len(first) < 3 {
			row := make(map[string]any, len(columns))
			for i, c := range columns {
				row[c] = formatScanValue(vals[i], colTypes[i])
			}
			first = append(first, row)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return first, count, count > cap, err
	}
	return first, count, count > cap, nil
}

// formatScanValue renders a driver-returned column value for display in
// ReplayResult.FirstRows. It switches on the uppercased SQL Server
// database type name so GUID / binary / text columns all come through as
// readable text instead of mojibake.
//
// Rules:
//   - UNIQUEIDENTIFIER + 16-byte []byte → canonical lowercase GUID.
//   - VARBINARY / BINARY / IMAGE / ROWVERSION / TIMESTAMP + []byte →
//     "0x" + uppercase hex.
//   - Text types (VARCHAR, NVARCHAR, CHAR, NCHAR, TEXT, NTEXT, XML,
//     SYSNAME) + []byte → string(b).
//   - Any other []byte: stringify when the content is valid UTF-8,
//     otherwise hex-encode. This keeps unknown driver surprises readable
//     without producing mojibake.
//   - Non-[]byte values pass through unchanged.
func formatScanValue(v any, dbType string) any {
	b, isBytes := v.([]byte)
	if !isBytes {
		return v
	}
	switch strings.ToUpper(dbType) {
	case "UNIQUEIDENTIFIER":
		if len(b) == 16 {
			return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
				b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
		}
		return hexLiteral(b)
	case "VARBINARY", "BINARY", "IMAGE", "ROWVERSION", "TIMESTAMP":
		return hexLiteral(b)
	case "VARCHAR", "NVARCHAR", "CHAR", "NCHAR", "TEXT", "NTEXT", "XML", "SYSNAME":
		return string(b)
	}
	if utf8.Valid(b) {
		return string(b)
	}
	return hexLiteral(b)
}

func hexLiteral(b []byte) string {
	return "0x" + strings.ToUpper(hex.EncodeToString(b))
}
