package client

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/flanksource/incident-commander/clientapi"
	"github.com/google/uuid"
	"github.com/timberio/go-datemath"
)

const (
	defaultJobRunLimit = 20
	maximumJobLimit    = 100
)

// JobRunListOptions filters persisted job execution history.
type JobRunListOptions struct {
	Name         string
	Statuses     []string
	ResourceType string
	ResourceID   string
	AgentID      string
	HasErrors    *bool
	Since        string
	Before       string
	Limit        int
}

type jobHistoryRow struct {
	ID             uuid.UUID      `json:"id"`
	AgentID        *uuid.UUID     `json:"agent_id"`
	Name           string         `json:"name"`
	SuccessCount   int            `json:"success_count"`
	ErrorCount     int            `json:"error_count"`
	Hostname       string         `json:"hostname"`
	DurationMillis int64          `json:"duration_millis"`
	ResourceType   string         `json:"resource_type"`
	ResourceID     string         `json:"resource_id"`
	Details        map[string]any `json:"details"`
	Status         string         `json:"status"`
	TimeStart      time.Time      `json:"time_start"`
	TimeEnd        *time.Time     `json:"time_end"`
}

// ListJobRuns returns persisted job executions, newest first.
func (c *Client) ListJobRuns(ctx context.Context, opts JobRunListOptions) ([]clientapi.JobRun, error) {
	limit, err := jobLimit(opts.Limit, defaultJobRunLimit)
	if err != nil {
		return nil, err
	}
	statuses, err := jobStatuses(opts.Statuses)
	if err != nil {
		return nil, err
	}

	request := c.R(ctx).
		QueryParam("order", "time_start.desc").
		QueryParam("limit", strconv.Itoa(limit)).
		QueryParam("select", "*")
	if opts.Name != "" {
		request = request.QueryParam("name", "ilike.*"+opts.Name+"*")
	}
	if len(statuses) > 0 {
		request = request.QueryParam("status", "in.("+strings.Join(statuses, ",")+")")
	}
	if opts.ResourceType != "" {
		request = request.QueryParam("resource_type", "eq."+opts.ResourceType)
	}
	if opts.ResourceID != "" {
		request = request.QueryParam("resource_id", "eq."+opts.ResourceID)
	}
	if opts.AgentID != "" {
		request = request.QueryParam("agent_id", "eq."+opts.AgentID)
	}
	if opts.HasErrors != nil {
		if *opts.HasErrors {
			request = request.QueryParam("error_count", "gt.0")
		} else {
			request = request.QueryParam("error_count", "eq.0")
		}
	}
	since, err := jobDateFilter(opts.Since)
	if err != nil {
		return nil, fmt.Errorf("invalid since: %w", err)
	}
	before, err := jobDateFilter(opts.Before)
	if err != nil {
		return nil, fmt.Errorf("invalid before: %w", err)
	}
	switch {
	case since != nil && before != nil:
		request = request.QueryParam("and", fmt.Sprintf("(time_start.gte.\"%s\",time_start.lte.\"%s\")",
			since.UTC().Format(time.RFC3339), before.UTC().Format(time.RFC3339)))
	case since != nil:
		request = request.QueryParam("time_start", "gte."+since.UTC().Format(time.RFC3339))
	case before != nil:
		request = request.QueryParam("time_start", "lte."+before.UTC().Format(time.RFC3339))
	}

	response, err := request.Get(c.apiPath("/db/job_history"))
	if err != nil {
		return nil, err
	}
	if !response.IsOK() {
		return nil, postgrestError(response)
	}

	var rows []jobHistoryRow
	if err := decodeJSON(response, &rows); err != nil {
		return nil, err
	}
	runs := make([]clientapi.JobRun, len(rows))
	for i, row := range rows {
		runs[i] = newJobRun(row, false)
	}
	return runs, nil
}

// GetJobRun returns the complete persisted record for a job execution.
func (c *Client) GetJobRun(ctx context.Context, id string) (*clientapi.JobRun, error) {
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("job run id is required")
	}

	response, err := c.R(ctx).
		QueryParam("id", "eq."+id).
		QueryParam("select", "*").
		QueryParam("limit", "1").
		Get(c.apiPath("/db/job_history"))
	if err != nil {
		return nil, err
	}
	if !response.IsOK() {
		return nil, postgrestError(response)
	}

	var rows []jobHistoryRow
	if err := decodeJSON(response, &rows); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("job run %s was not found: %w", id, ErrNotFound)
	}
	run := newJobRun(rows[0], true)
	return &run, nil
}

func jobLimit(limit, defaultLimit int) (int, error) {
	if limit == 0 {
		limit = defaultLimit
	}
	if limit < 1 {
		return 0, fmt.Errorf("limit must be at least 1")
	}
	if limit > maximumJobLimit {
		return maximumJobLimit, nil
	}
	return limit, nil
}

func jobStatuses(statuses []string) ([]string, error) {
	valid := map[string]bool{
		clientapi.JobStatusRunning: true,
		clientapi.JobStatusSuccess: true,
		clientapi.JobStatusWarning: true,
		clientapi.JobStatusFailed:  true,
		clientapi.JobStatusStale:   true,
		clientapi.JobStatusSkipped: true,
	}

	out := make([]string, 0, len(statuses))
	for _, status := range statuses {
		for _, part := range strings.Split(status, ",") {
			part = strings.ToUpper(strings.TrimSpace(part))
			if part == "" {
				continue
			}
			if !valid[part] {
				return nil, fmt.Errorf("invalid status %q", part)
			}
			out = append(out, part)
		}
	}
	return out, nil
}

func jobDateFilter(value string) (*time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	expression, err := datemath.Parse(value)
	if err != nil {
		return nil, err
	}
	parsed := expression.Time()
	return &parsed, nil
}

func newJobRun(row jobHistoryRow, includeDetails bool) clientapi.JobRun {
	run := clientapi.JobRun{
		ID:             row.ID.String(),
		Name:           row.Name,
		Status:         row.Status,
		SuccessCount:   row.SuccessCount,
		ErrorCount:     row.ErrorCount,
		Hostname:       row.Hostname,
		DurationMillis: row.DurationMillis,
		ResourceType:   row.ResourceType,
		ResourceID:     row.ResourceID,
		TimeStart:      row.TimeStart,
		TimeEnd:        row.TimeEnd,
		Errors:         jobRunErrors(row.Details),
	}
	if row.AgentID != nil && *row.AgentID != uuid.Nil {
		run.AgentID = row.AgentID.String()
	}
	if includeDetails {
		run.Details = row.Details
	}
	return run
}

func jobRunErrors(details map[string]any) []string {
	if details == nil {
		return nil
	}

	value, ok := details["errors"]
	if !ok {
		return nil
	}
	switch errors := value.(type) {
	case []string:
		return errors
	case []any:
		result := make([]string, len(errors))
		for i, err := range errors {
			result[i] = fmt.Sprint(err)
		}
		return result
	case string:
		return []string{errors}
	default:
		return []string{fmt.Sprint(errors)}
	}
}
