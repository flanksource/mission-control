// Package xetrace manages short-lived SQL Server Extended Events sessions
// backed by a ring_buffer target. It is designed for interactive CLI use:
// create a session, poll it for a bounded window, and tear it down
// unconditionally.
package xetrace

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"gorm.io/gorm"
)

// Event names supported by CreateOptions.Events.
const (
	EventSQLStatementCompleted = "sql_statement_completed"
	EventRPCCompleted          = "rpc_completed"
	EventSQLBatchCompleted     = "sql_batch_completed"
	EventErrorReported         = "error_reported"
)

// DefaultEvents is the set captured when CreateOptions.Events is empty.
var DefaultEvents = []string{
	EventSQLStatementCompleted,
	EventRPCCompleted,
	EventSQLBatchCompleted,
	EventErrorReported,
}

// CreateOptions configures a new Extended Events session.
type CreateOptions struct {
	// Name of the XE session. If empty, a unique name is generated.
	Name string
	// DatabaseName scopes the session to a single database. Empty means
	// "all databases on the instance".
	DatabaseName string
	// MinDurationMicros filters out events faster than this threshold.
	// Only applied to duration-bearing events (statement/rpc/batch).
	MinDurationMicros int64
	// Events is the list of XE event names to capture. When empty,
	// DefaultEvents is used.
	Events []string
	// ExcludeSessionID, when non-zero, excludes events emitted by that
	// specific sqlserver session_id. Used to hide the tracer's own SQL.
	ExcludeSessionID int
	// MaxMemoryKB is the ring buffer size. Defaults to 4096 when zero.
	MaxMemoryKB int
	// MaxEvents caps the ring buffer event count. Defaults to 1000 when zero.
	MaxEvents int
}

// Session represents a live XE session owned by this process.
type Session struct {
	Name string
	db   *gorm.DB
	opts CreateOptions
}

// Create builds an XE session with a ring_buffer target, starts it, and
// returns a Session handle. Callers MUST defer s.Drop to avoid leaking the
// session on the server.
func Create(ctx context.Context, db *gorm.DB, opts CreateOptions) (*Session, error) {
	if opts.Name == "" {
		opts.Name = fmt.Sprintf("mc_xe_trace_%d_%d", os.Getpid(), time.Now().Unix())
	}
	if len(opts.Events) == 0 {
		opts.Events = DefaultEvents
	}
	if opts.MaxMemoryKB == 0 {
		opts.MaxMemoryKB = 4096
	}
	if opts.MaxEvents == 0 {
		opts.MaxEvents = 1000
	}

	ddl, err := BuildCreateSQL(opts)
	if err != nil {
		return nil, err
	}

	// Every db.Exec below flows through gormlog.SqlLogger via the gorm
	// logger.Interface hook — no hand-rolled log lines needed.
	if err := db.WithContext(ctx).Exec(ddl).Error; err != nil {
		return nil, fmt.Errorf("create event session %q: %w", opts.Name, err)
	}

	start := fmt.Sprintf("ALTER EVENT SESSION %s ON SERVER STATE = START", quoteIdent(opts.Name))
	if err := db.WithContext(ctx).Exec(start).Error; err != nil {
		// Best-effort cleanup of the half-built session.
		_ = db.Exec(fmt.Sprintf("DROP EVENT SESSION %s ON SERVER", quoteIdent(opts.Name))).Error
		return nil, fmt.Errorf("start event session %q: %w", opts.Name, err)
	}

	return &Session{Name: opts.Name, db: db, opts: opts}, nil
}

// Drop stops and removes the session. Safe to call on a nil receiver. Uses a
// fresh timeout-bounded context so it still runs during shutdown when the
// caller context has already been cancelled.
func (s *Session) Drop(parent context.Context) error {
	if s == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = parent // retained for signature symmetry; we intentionally do not use it
	stmt := fmt.Sprintf("DROP EVENT SESSION %s ON SERVER", quoteIdent(s.Name))
	if err := s.db.WithContext(ctx).Exec(stmt).Error; err != nil {
		return fmt.Errorf("drop event session %q: %w", s.Name, err)
	}
	return nil
}

// Poll reads the current ring_buffer contents and returns the parsed events.
// Callers are responsible for deduplication via Event.Key across polls.
func (s *Session) Poll(ctx context.Context) ([]Event, error) {
	const q = `SELECT CAST(target_data AS NVARCHAR(MAX)) AS target_data
FROM sys.dm_xe_sessions s
JOIN sys.dm_xe_session_targets t ON t.event_session_address = s.address
WHERE s.name = ? AND t.target_name = 'ring_buffer'`

	var payload string
	row := s.db.WithContext(ctx).Raw(q, s.Name).Row()
	if err := row.Scan(&payload); err != nil {
		return nil, fmt.Errorf("read ring_buffer target: %w", err)
	}
	if payload == "" {
		return nil, nil
	}
	return ParseRingBuffer(payload)
}

// CurrentSessionID returns the sqlserver session_id of the current connection,
// so callers can pass it as CreateOptions.ExcludeSessionID.
func CurrentSessionID(ctx context.Context, db *gorm.DB) (int, error) {
	var sid int
	if err := db.WithContext(ctx).Raw("SELECT @@SPID").Row().Scan(&sid); err != nil {
		return 0, fmt.Errorf("query @@SPID: %w", err)
	}
	return sid, nil
}

// CurrentDatabase returns DB_NAME() for the current connection.
func CurrentDatabase(ctx context.Context, db *gorm.DB) (string, error) {
	var name string
	if err := db.WithContext(ctx).Raw("SELECT DB_NAME()").Row().Scan(&name); err != nil {
		return "", fmt.Errorf("query DB_NAME(): %w", err)
	}
	return name, nil
}

// quoteIdent wraps a SQL Server identifier in brackets, escaping any embedded
// closing bracket. Used for session names, which we generate ourselves but
// still want to keep safe.
func quoteIdent(name string) string {
	return "[" + strings.ReplaceAll(name, "]", "]]") + "]"
}
