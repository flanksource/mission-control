package pgsessions

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

type Session struct {
	PID             int       `json:"pid"`
	User            string    `json:"user"`
	Database        string    `json:"database"`
	ApplicationName string    `json:"applicationName,omitempty"`
	ClientAddr      string    `json:"clientAddr,omitempty"`
	State           string    `json:"state"`
	WaitEventType   string    `json:"waitEventType,omitempty"`
	WaitEvent       string    `json:"waitEvent,omitempty"`
	QueryStart      time.Time `json:"queryStart,omitempty"`
	StateChange     time.Time `json:"stateChange,omitempty"`
	DurationMS      int64     `json:"durationMs,omitempty"`
	BlockedBy       []int     `json:"blockedBy,omitempty"`
	Query           string    `json:"query,omitempty"`
}

type row struct {
	PID             int        `gorm:"column:pid"`
	User            *string    `gorm:"column:usename"`
	Database        *string    `gorm:"column:datname"`
	ApplicationName *string    `gorm:"column:application_name"`
	ClientAddr      *string    `gorm:"column:client_addr"`
	State           *string    `gorm:"column:state"`
	WaitEventType   *string    `gorm:"column:wait_event_type"`
	WaitEvent       *string    `gorm:"column:wait_event"`
	QueryStart      *time.Time `gorm:"column:query_start"`
	StateChange     *time.Time `gorm:"column:state_change"`
	DurationMS      *float64   `gorm:"column:duration_ms"`
	BlockedBy       *string    `gorm:"column:blocked_by"`
	Query           *string    `gorm:"column:query"`
}

func List(ctx context.Context, db *gorm.DB, database string, includeIdle bool) ([]Session, error) {
	var rows []row
	includeIdleFlag := 0
	if includeIdle {
		includeIdleFlag = 1
	}
	if err := db.WithContext(ctx).Raw(`
SELECT pid, usename, datname, application_name, client_addr::text AS client_addr,
       state, wait_event_type, wait_event, query_start, state_change,
       EXTRACT(EPOCH FROM (now() - COALESCE(query_start, state_change))) * 1000 AS duration_ms,
       array_to_string(pg_blocking_pids(pid), ',') AS blocked_by,
       query
FROM pg_stat_activity
WHERE pid <> pg_backend_pid()
  AND (? = '' OR datname = ?)
  AND (? = 1 OR state <> 'idle')
ORDER BY
  CASE WHEN state = 'active' THEN 0 ELSE 1 END,
  COALESCE(query_start, state_change) NULLS LAST
`, database, database, includeIdleFlag).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	out := make([]Session, 0, len(rows))
	for _, r := range rows {
		out = append(out, Session{
			PID:             r.PID,
			User:            strDeref(r.User),
			Database:        strDeref(r.Database),
			ApplicationName: strDeref(r.ApplicationName),
			ClientAddr:      strDeref(r.ClientAddr),
			State:           strDeref(r.State),
			WaitEventType:   strDeref(r.WaitEventType),
			WaitEvent:       strDeref(r.WaitEvent),
			QueryStart:      timeDeref(r.QueryStart),
			StateChange:     timeDeref(r.StateChange),
			DurationMS:      int64(float64Deref(r.DurationMS)),
			BlockedBy:       parsePIDList(strDeref(r.BlockedBy)),
			Query:           strDeref(r.Query),
		})
	}
	return out, nil
}

func Cancel(ctx context.Context, db *gorm.DB, pid int) (bool, error) {
	return backendAction(ctx, db, "pg_cancel_backend", pid)
}

func Terminate(ctx context.Context, db *gorm.DB, pid int) (bool, error) {
	return backendAction(ctx, db, "pg_terminate_backend", pid)
}

func backendAction(ctx context.Context, db *gorm.DB, fn string, pid int) (bool, error) {
	if pid <= 0 {
		return false, fmt.Errorf("invalid pid %d", pid)
	}
	var ok bool
	if err := db.WithContext(ctx).Raw("SELECT "+fn+"(?)", pid).Scan(&ok).Error; err != nil {
		return false, fmt.Errorf("%s(%d): %w", fn, pid, err)
	}
	return ok, nil
}

func parsePIDList(s string) []int {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err == nil && n > 0 {
			out = append(out, n)
		}
	}
	return out
}

func strDeref(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func timeDeref(v *time.Time) time.Time {
	if v == nil {
		return time.Time{}
	}
	return *v
}

func float64Deref(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}
