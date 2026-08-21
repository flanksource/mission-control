package clientapi

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

const SourceUI = "UI"

type PlaybookListItem struct {
	ID          uuid.UUID       `json:"id"`
	Namespace   string          `json:"namespace,omitempty"`
	Name        string          `json:"name"`
	Title       string          `json:"title,omitempty"`
	Icon        string          `json:"icon,omitempty"`
	Description string          `json:"description,omitempty"`
	Source      string          `json:"source,omitempty"`
	Category    string          `json:"category,omitempty"`
	CreatedAt   *time.Time      `json:"created_at,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Spec        json.RawMessage `json:"spec,omitempty"`
}

type PlaybookParameter struct {
	Name        string          `json:"name"`
	Default     string          `json:"default,omitempty"`
	Label       string          `json:"label,omitempty"`
	Required    bool            `json:"required,omitempty"`
	Icon        string          `json:"icon,omitempty"`
	Description string          `json:"description,omitempty"`
	Type        string          `json:"type,omitempty"`
	Properties  json.RawMessage `json:"properties,omitempty"`
	DependsOn   []string        `json:"dependsOn,omitempty"`
}

type PlaybookSpecSummary struct {
	Title       string                       `json:"title,omitempty"`
	Description string                       `json:"description,omitempty"`
	Category    string                       `json:"category,omitempty"`
	Icon        string                       `json:"icon,omitempty"`
	Configs     []json.RawMessage            `json:"configs,omitempty"`
	Checks      []json.RawMessage            `json:"checks,omitempty"`
	Components  []json.RawMessage            `json:"components,omitempty"`
	Parameters  []PlaybookParameter          `json:"parameters,omitempty"`
	Actions     []map[string]json.RawMessage `json:"actions,omitempty"`
}

type Playbook struct {
	ID          uuid.UUID       `json:"id"`
	Namespace   string          `json:"namespace"`
	Name        string          `json:"name"`
	Title       string          `json:"title"`
	Icon        string          `json:"icon,omitempty"`
	Description string          `json:"description,omitempty"`
	Spec        json.RawMessage `json:"spec"`
	Source      string          `json:"source"`
	Category    string          `json:"category"`
	CreatedBy   *uuid.UUID      `json:"created_by,omitempty"`
	CreatedAt   time.Time       `json:"created_at,omitempty"`
	UpdatedAt   time.Time       `json:"updated_at,omitempty"`
	DeletedAt   *time.Time      `json:"deleted_at,omitempty"`
}

type PlaybookRunStatus string

const (
	PlaybookRunStatusCancelled       PlaybookRunStatus = "cancelled"
	PlaybookRunStatusTimedOut        PlaybookRunStatus = "timed_out"
	PlaybookRunStatusCompleted       PlaybookRunStatus = "completed"
	PlaybookRunStatusFailed          PlaybookRunStatus = "failed"
	PlaybookRunStatusPendingApproval PlaybookRunStatus = "pending_approval"
	PlaybookRunStatusRunning         PlaybookRunStatus = "running"
	PlaybookRunStatusScheduled       PlaybookRunStatus = "scheduled"
	PlaybookRunStatusSleeping        PlaybookRunStatus = "sleeping"
	PlaybookRunStatusRetrying        PlaybookRunStatus = "retrying"
	PlaybookRunStatusWaiting         PlaybookRunStatus = "waiting"
)

func (s PlaybookRunStatus) Final() bool {
	switch s {
	case PlaybookRunStatusCancelled, PlaybookRunStatusCompleted, PlaybookRunStatusFailed, PlaybookRunStatusTimedOut:
		return true
	default:
		return false
	}
}

type PlaybookRun struct {
	ID                 uuid.UUID         `json:"id"`
	PlaybookID         uuid.UUID         `json:"playbook_id"`
	Status             PlaybookRunStatus `json:"status,omitempty"`
	Spec               json.RawMessage   `json:"spec"`
	CreatedAt          time.Time         `json:"created_at,omitempty"`
	StartTime          *time.Time        `json:"start_time,omitempty"`
	ScheduledTime      time.Time         `json:"scheduled_time,omitempty"`
	EndTime            *time.Time        `json:"end_time,omitempty"`
	Timeout            time.Duration     `json:"timeout,omitempty"`
	CreatedBy          *uuid.UUID        `json:"created_by,omitempty"`
	ComponentID        *uuid.UUID        `json:"component_id,omitempty"`
	CheckID            *uuid.UUID        `json:"check_id,omitempty"`
	ConfigID           *uuid.UUID        `json:"config_id,omitempty"`
	Error              *string           `json:"error,omitempty"`
	Parameters         map[string]string `json:"parameters,omitempty"`
	Request            map[string]any    `json:"request,omitempty"`
	AgentID            *uuid.UUID        `json:"agent_id,omitempty"`
	ParentID           *uuid.UUID        `json:"parent_id,omitempty"`
	NotificationSendID *uuid.UUID        `json:"notification_send_id,omitempty"`
}

type PlaybookActionStatus string

type PlaybookRunAction struct {
	ID            uuid.UUID            `json:"id"`
	Name          string               `json:"name"`
	PlaybookRunID uuid.UUID            `json:"playbook_run_id"`
	Status        PlaybookActionStatus `json:"status,omitempty"`
	ScheduledTime time.Time            `json:"scheduled_time,omitempty"`
	StartTime     time.Time            `json:"start_time,omitempty"`
	EndTime       *time.Time           `json:"end_time,omitempty"`
	Result        map[string]any       `json:"result,omitempty"`
	Error         *string              `json:"error,omitempty"`
	IsPushed      bool                 `json:"is_pushed"`
	AgentID       *uuid.UUID           `json:"agent_id,omitempty"`
	RetryCount    int                  `json:"attempt,omitempty"`
}

type PlaybookSummary struct {
	Playbook Playbook            `json:"playbook,omitempty"`
	Run      PlaybookRun         `json:"run,omitempty"`
	Actions  []PlaybookRunAction `json:"actions,omitempty"`
}

type PlaybookApplyRequest struct {
	Namespace string          `json:"namespace"`
	Name      string          `json:"name"`
	Spec      json.RawMessage `json:"spec"`
}

type PlaybookApplyResponse struct {
	Playbook Playbook `json:"playbook"`
	Created  bool     `json:"created"`
}

type PlaybookSQLResult struct {
	Query   string           `json:"query,omitempty"`
	Rows    []map[string]any `json:"rows,omitempty"`
	Count   int              `json:"count"`
	Columns []string         `json:"columns,omitempty"`
}

type PlaybookExecResult struct {
	Stdout   string         `json:"stdout"`
	Stderr   string         `json:"stderr"`
	ExitCode int            `json:"exitCode"`
	Path     string         `json:"path"`
	Args     []string       `json:"args"`
	Extra    map[string]any `json:"extra,omitempty"`
}

type PlaybookHTTPResult struct {
	Content    string            `json:"content"`
	Headers    map[string]string `json:"headers"`
	StatusCode int               `json:"code"`
}
