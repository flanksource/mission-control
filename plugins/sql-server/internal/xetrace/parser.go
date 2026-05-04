package xetrace

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Event is a decoded row from a ring_buffer target.
type Event struct {
	Name          string        `json:"name"`
	Timestamp     time.Time     `json:"timestamp"`
	Duration      time.Duration `json:"duration"`
	CPUTime       time.Duration `json:"cpu_time"`
	LogicalReads  int64         `json:"logical_reads"`
	PhysicalReads int64         `json:"physical_reads"`
	Writes        int64         `json:"writes"`
	RowCount      int64         `json:"row_count"`
	DatabaseName  string        `json:"database_name"`
	ClientApp     string        `json:"client_app_name"`
	Username      string        `json:"username"`
	SessionID     int           `json:"session_id"`
	Statement     string        `json:"raw_statement,omitempty"`
	SQL           string        `json:"statement"`
	ErrorNumber   int           `json:"error_number,omitempty"`
	ErrorMessage  string        `json:"error_message,omitempty"`
}

// MergedStatement returns the SQL with parameters inlined. For RPC events
// (sp_prepexec, sp_executesql) this unwraps the scaffold and substitutes
// @P0/@P1/… with their literal values. For plain statements it returns
// the whitespace-collapsed original.
func (e Event) MergedStatement() string {
	stmt := collapseWhitespace(strings.TrimSpace(e.Statement))
	if unwrapped, ok := UnwrapRPC(stmt); ok {
		return unwrapped
	}
	return stmt
}

// Key returns a string uniquely identifying this event within a ring buffer.
// Used by callers to deduplicate across overlapping polls.
func (e Event) Key() string {
	return fmt.Sprintf("%s|%d|%d|%s", e.Timestamp.Format(time.RFC3339Nano), e.SessionID, int64(e.Duration), firstN(e.Statement, 64))
}

type ringBufferTarget struct {
	XMLName xml.Name      `xml:"RingBufferTarget"`
	Events  []rawXMLEvent `xml:"event"`
}

type rawXMLEvent struct {
	Name      string        `xml:"name,attr"`
	Timestamp string        `xml:"timestamp,attr"`
	Data      []rawXMLField `xml:"data"`
	Actions   []rawXMLField `xml:"action"`
}

type rawXMLField struct {
	Name  string `xml:"name,attr"`
	Type  string `xml:"type,attr"`
	Value string `xml:"value"`
	Text  string `xml:"text"`
}

// ParseRingBuffer decodes a ring_buffer target_data payload into []Event.
// Exported so it can be unit-tested against captured fixtures without a DB.
func ParseRingBuffer(payload string) ([]Event, error) {
	var rb ringBufferTarget
	if err := xml.Unmarshal([]byte(payload), &rb); err != nil {
		return nil, fmt.Errorf("decode ring_buffer xml: %w", err)
	}
	out := make([]Event, 0, len(rb.Events))
	for _, raw := range rb.Events {
		e := toEvent(raw)
		if isNoiseStatement(e.Statement) {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

func toEvent(raw rawXMLEvent) Event {
	e := Event{Name: raw.Name}
	if t, err := time.Parse(time.RFC3339Nano, raw.Timestamp); err == nil {
		e.Timestamp = t
	} else if t, err := time.Parse("2006-01-02T15:04:05.999Z", raw.Timestamp); err == nil {
		e.Timestamp = t
	}

	for _, f := range raw.Data {
		applyField(&e, f)
	}
	for _, f := range raw.Actions {
		applyField(&e, f)
	}
	e.SQL = e.MergedStatement()
	return e
}

func applyField(e *Event, f rawXMLField) {
	val := f.Value
	if val == "" {
		val = f.Text
	}
	switch f.Name {
	case "duration":
		e.Duration = time.Duration(parseInt64(val)) * time.Microsecond
	case "cpu_time":
		e.CPUTime = time.Duration(parseInt64(val)) * time.Microsecond
	case "logical_reads":
		e.LogicalReads = parseInt64(val)
	case "physical_reads":
		e.PhysicalReads = parseInt64(val)
	case "writes":
		e.Writes = parseInt64(val)
	case "row_count":
		e.RowCount = parseInt64(val)
	case "statement", "batch_text", "sql_text":
		if e.Statement == "" {
			e.Statement = val
		}
	case "database_name":
		e.DatabaseName = val
	case "client_app_name":
		e.ClientApp = val
	case "username":
		e.Username = val
	case "session_id":
		e.SessionID = int(parseInt64(val))
	case "error_number":
		e.ErrorNumber = int(parseInt64(val))
	case "message":
		e.ErrorMessage = val
	}
}

func parseInt64(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// noiseStatements is the canonicalized (upper-case, whitespace-collapsed,
// trailing-semicolon-stripped) set of SQL Server driver chatter that
// ParseRingBuffer drops. Extend this set — not a substring matcher — when
// new pure-noise statements appear in traces.
var noiseStatements = map[string]struct{}{
	"IF @@TRANCOUNT > 0":             {},
	"IF @@TRANCOUNT > 0 COMMIT TRAN": {},
	"COMMIT TRAN":                    {},
	"SELECT 1":                       {},
}

// noisePrefixes are canonicalized statement prefixes that are always noise
// regardless of trailing arguments. Used for driver chatter whose payload
// varies (e.g. sp_unprepare takes a handle id, SET TEXTSIZE takes a
// byte count, SET QUOTED_IDENTIFIER takes ON/OFF).
var noisePrefixes = []string{
	"EXEC SP_UNPREPARE ",
	"SET QUOTED_IDENTIFIER ",
	"SET TEXTSIZE ",
	"SET ARITHABORT ",
	"SET NUMERIC_ROUNDABORT ",
	"SET ANSI_NULLS ",
	"SET ANSI_NULL_DFLT_ON ",
	"SET ANSI_PADDING ",
	"SET ANSI_WARNINGS ",
	"SET CONCAT_NULL_YIELDS_NULL ",
	"SET CURSOR_CLOSE_ON_COMMIT ",
	"SET IMPLICIT_TRANSACTIONS ",
	"SET LOCK_TIMEOUT ",
	"SET DATEFORMAT ",
	"SET DATEFIRST ",
	"SET LANGUAGE ",
	"SET NOCOUNT ",
	"SET TRANSACTION ISOLATION LEVEL ",
	"SET XACT_ABORT ",
	"SET DEADLOCK_PRIORITY ",
	"SET ROWCOUNT ",
	"SET FMTONLY ",
	"SET NO_BROWSETABLE ",
}

// isNoiseStatement reports whether stmt is a known driver chatter statement
// that should be dropped from trace output. Exact-match only after
// normalization — real queries that happen to contain these tokens as
// substrings (e.g. "SELECT 1 FROM dual") are preserved.
func isNoiseStatement(stmt string) bool {
	s := collapseWhitespace(strings.TrimSpace(stmt))
	s = strings.TrimSuffix(s, ";")
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	upper := strings.ToUpper(s)
	if _, ok := noiseStatements[upper]; ok {
		return true
	}
	for _, p := range noisePrefixes {
		if strings.HasPrefix(upper, p) {
			return true
		}
	}
	return false
}
