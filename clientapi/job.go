package clientapi

import "time"

const (
	JobStatusRunning = "RUNNING"
	JobStatusSuccess = "SUCCESS"
	JobStatusWarning = "WARNING"
	JobStatusFailed  = "FAILED"
	JobStatusStale   = "STALE"
	JobStatusSkipped = "SKIPPED"
)

// JobRun is a persisted job execution from /db/job_history.
type JobRun struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Status         string         `json:"status"`
	SuccessCount   int            `json:"success_count"`
	ErrorCount     int            `json:"error_count"`
	Hostname       string         `json:"hostname,omitempty"`
	DurationMillis int64          `json:"duration_millis"`
	ResourceType   string         `json:"resource_type,omitempty"`
	ResourceID     string         `json:"resource_id,omitempty"`
	AgentID        string         `json:"agent_id,omitempty"`
	TimeStart      time.Time      `json:"time_start"`
	TimeEnd        *time.Time     `json:"time_end,omitempty"`
	Errors         []string       `json:"errors,omitempty"`
	Details        map[string]any `json:"details,omitempty"`
}
