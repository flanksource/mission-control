package pglocks

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

type Lock struct {
	PID       int    `json:"pid"`
	Database  string `json:"database,omitempty"`
	User      string `json:"user,omitempty"`
	State     string `json:"state,omitempty"`
	LockType  string `json:"lockType"`
	Mode      string `json:"mode"`
	Granted   bool   `json:"granted"`
	Relation  string `json:"relation,omitempty"`
	Page      *int64 `json:"page,omitempty"`
	Tuple     *int64 `json:"tuple,omitempty"`
	BlockedBy []int  `json:"blockedBy,omitempty"`
	WaitEvent string `json:"waitEvent,omitempty"`
	Query     string `json:"query,omitempty"`
	AgeMS     int64  `json:"ageMs,omitempty"`
}

type row struct {
	PID      int      `gorm:"column:pid"`
	Database *string  `gorm:"column:datname"`
	User     *string  `gorm:"column:usename"`
	State    *string  `gorm:"column:state"`
	LockType string   `gorm:"column:locktype"`
	Mode     string   `gorm:"column:mode"`
	Granted  bool     `gorm:"column:granted"`
	Relation *string  `gorm:"column:relation"`
	Page     *int64   `gorm:"column:page"`
	Tuple    *int64   `gorm:"column:tuple"`
	Blocked  *string  `gorm:"column:blocked_by"`
	Wait     *string  `gorm:"column:wait_event"`
	Query    *string  `gorm:"column:query"`
	AgeMS    *float64 `gorm:"column:age_ms"`
}

func List(ctx context.Context, db *gorm.DB, database string, onlyBlocked bool) ([]Lock, error) {
	blockedFlag := 0
	if onlyBlocked {
		blockedFlag = 1
	}
	var rows []row
	if err := db.WithContext(ctx).Raw(`
SELECT l.pid, a.datname, a.usename, a.state, l.locktype, l.mode, l.granted,
       l.relation::regclass::text AS relation, l.page, l.tuple,
       array_to_string(pg_blocking_pids(l.pid), ',') AS blocked_by,
       concat_ws(':', a.wait_event_type, a.wait_event) AS wait_event,
       a.query,
       EXTRACT(EPOCH FROM (now() - COALESCE(a.query_start, a.state_change))) * 1000 AS age_ms
FROM pg_locks l
LEFT JOIN pg_stat_activity a ON a.pid = l.pid
WHERE l.pid <> pg_backend_pid()
  AND (? = '' OR a.datname = ?)
  AND (? = 0 OR NOT l.granted OR cardinality(pg_blocking_pids(l.pid)) > 0)
ORDER BY l.granted, age_ms DESC NULLS LAST, l.pid
`, database, database, blockedFlag).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list locks: %w", err)
	}
	out := make([]Lock, 0, len(rows))
	for _, r := range rows {
		out = append(out, Lock{
			PID:       r.PID,
			Database:  strDeref(r.Database),
			User:      strDeref(r.User),
			State:     strDeref(r.State),
			LockType:  r.LockType,
			Mode:      r.Mode,
			Granted:   r.Granted,
			Relation:  strDeref(r.Relation),
			Page:      r.Page,
			Tuple:     r.Tuple,
			BlockedBy: parsePIDList(strDeref(r.Blocked)),
			WaitEvent: strDeref(r.Wait),
			Query:     strDeref(r.Query),
			AgeMS:     int64(float64Deref(r.AgeMS)),
		})
	}
	return out, nil
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

func float64Deref(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}
