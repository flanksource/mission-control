package xetrace

import (
	"fmt"
	"sort"
	"strings"
)

// eventHasDuration reports whether an XE event exposes a `duration` field we
// can filter on.
func eventHasDuration(name string) bool {
	switch name {
	case EventSQLStatementCompleted, EventRPCCompleted, EventSQLBatchCompleted:
		return true
	default:
		return false
	}
}

// BuildCreateSQL assembles the CREATE EVENT SESSION DDL for the given options.
// Exported for testing.
func BuildCreateSQL(opts CreateOptions) (string, error) {
	if opts.Name == "" {
		return "", fmt.Errorf("session name is required")
	}
	if len(opts.Events) == 0 {
		return "", fmt.Errorf("at least one event is required")
	}

	events := append([]string(nil), opts.Events...)
	sort.Strings(events)

	var b strings.Builder
	fmt.Fprintf(&b, "CREATE EVENT SESSION %s ON SERVER\n", quoteIdent(opts.Name))

	for i, name := range events {
		if i > 0 {
			b.WriteString(",\n")
		}
		writeEventClause(&b, name, opts)
	}

	fmt.Fprintf(&b, "\nADD TARGET package0.ring_buffer (SET max_memory = %d, max_events_limit = %d)", opts.MaxMemoryKB, opts.MaxEvents)
	b.WriteString("\nWITH (MAX_DISPATCH_LATENCY = 1 SECONDS, TRACK_CAUSALITY = OFF, STARTUP_STATE = OFF)")
	return b.String(), nil
}

func writeEventClause(b *strings.Builder, name string, opts CreateOptions) {
	fmt.Fprintf(b, "ADD EVENT sqlserver.%s (\n", name)

	// Actions: extra columns we want alongside the event's intrinsic fields.
	actions := []string{
		"sqlserver.client_app_name",
		"sqlserver.database_name",
		"sqlserver.session_id",
		"sqlserver.sql_text",
		"sqlserver.username",
	}
	fmt.Fprintf(b, "    ACTION (%s)", strings.Join(actions, ", "))

	preds := buildPredicates(name, opts)
	if len(preds) > 0 {
		fmt.Fprintf(b, "\n    WHERE (%s)", strings.Join(preds, " AND "))
	}
	b.WriteString("\n)")
}

func buildPredicates(event string, opts CreateOptions) []string {
	var preds []string
	if opts.DatabaseName != "" {
		preds = append(preds, fmt.Sprintf("sqlserver.database_name = N'%s'", escapeSQLStringLiteral(opts.DatabaseName)))
	}
	if opts.ExcludeSessionID > 0 {
		preds = append(preds, fmt.Sprintf("sqlserver.session_id <> %d", opts.ExcludeSessionID))
	}
	if opts.MinDurationMicros > 0 && eventHasDuration(event) {
		preds = append(preds, fmt.Sprintf("duration >= %d", opts.MinDurationMicros))
	}
	return preds
}

func escapeSQLStringLiteral(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}
