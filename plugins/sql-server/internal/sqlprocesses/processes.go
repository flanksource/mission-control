// Package sqlprocesses lists active SQL Server sessions and kills them on
// request.
package sqlprocesses

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
)

type Process struct {
	SessionID    int           `json:"sessionId"`
	Status       string        `json:"status"`
	Login        string        `json:"login"`
	HostName     string        `json:"host"`
	Program      string        `json:"program"`
	Database     string        `json:"database"`
	Command      string        `json:"command"`
	BlockedBy    int           `json:"blockedBy"`
	WaitType     string        `json:"waitType,omitempty"`
	WaitDuration time.Duration `json:"waitDuration"`
	CPUTime      time.Duration `json:"cpuTime"`
	LogicalReads int64         `json:"logicalReads"`
	LastBatch    time.Time     `json:"lastBatch"`
	SQL          string        `json:"sql,omitempty"`
}

type processRow struct {
	SessionID    int
	Status       string
	Login        string
	HostName     *string
	Program      *string
	Database     *string
	Command      *string
	BlockedBy    int
	WaitType     *string
	WaitMillis   int64
	CPUMillis    int64
	LogicalReads int64
	LastBatch    time.Time
	SQL          *string
}

func (r processRow) toProcess() Process {
	return Process{
		SessionID:    r.SessionID,
		Status:       r.Status,
		Login:        r.Login,
		HostName:     strDeref(r.HostName),
		Program:      strDeref(r.Program),
		Database:     strDeref(r.Database),
		Command:      strDeref(r.Command),
		BlockedBy:    r.BlockedBy,
		WaitType:     strDeref(r.WaitType),
		WaitDuration: time.Duration(r.WaitMillis) * time.Millisecond,
		CPUTime:      time.Duration(r.CPUMillis) * time.Millisecond,
		LogicalReads: r.LogicalReads,
		LastBatch:    r.LastBatch,
		SQL:          strDeref(r.SQL),
	}
}

const processesQuery = `
SELECT
    s.session_id                                   AS session_id,
    ISNULL(r.status, s.status)                     AS status,
    s.login_name                                   AS login,
    s.host_name                                    AS host_name,
    s.program_name                                 AS program,
    DB_NAME(ISNULL(r.database_id, s.database_id))  AS database_name,
    r.command                                      AS command,
    ISNULL(r.blocking_session_id, 0)               AS blocked_by,
    r.wait_type                                    AS wait_type,
    ISNULL(r.wait_time, 0)                         AS wait_millis,
    ISNULL(r.cpu_time, s.cpu_time)                 AS cpu_millis,
    ISNULL(r.logical_reads, s.logical_reads)       AS logical_reads,
    s.last_request_end_time                        AS last_batch,
    t.text                                         AS sql_text
FROM sys.dm_exec_sessions s
LEFT JOIN sys.dm_exec_requests r ON r.session_id = s.session_id
OUTER APPLY sys.dm_exec_sql_text(r.sql_handle) t
WHERE s.is_user_process = 1
  AND (? = '' OR DB_NAME(ISNULL(r.database_id, s.database_id)) = ?)
  AND (? = 1 OR LOWER(ISNULL(r.status, s.status)) <> 'sleeping')
ORDER BY
    CASE WHEN r.session_id IS NULL THEN 1 ELSE 0 END,
    ISNULL(r.cpu_time, 0) DESC,
    s.session_id
`

// List returns one row per active user session on the instance.
func List(ctx context.Context, db *gorm.DB, dbFilter string, includeSleeping bool) ([]Process, error) {
	var rows []processRow
	includeSleepingFlag := 0
	if includeSleeping {
		includeSleepingFlag = 1
	}
	if err := db.WithContext(ctx).Raw(processesQuery, dbFilter, dbFilter, includeSleepingFlag).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list processes: %w", err)
	}
	out := make([]Process, len(rows))
	for i, r := range rows {
		out[i] = r.toProcess()
	}
	return out, nil
}

// Kill issues KILL <spid> against the instance. KILL is not recoverable —
// the caller is expected to confirm the intent before invoking.
func Kill(ctx context.Context, db *gorm.DB, sessionID int) error {
	if sessionID <= 0 {
		return fmt.Errorf("invalid session id %d", sessionID)
	}
	stmt := fmt.Sprintf("KILL %d", sessionID)
	if err := db.WithContext(ctx).Exec(stmt).Error; err != nil {
		return fmt.Errorf("kill session %d: %w", sessionID, err)
	}
	return nil
}

func strDeref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
