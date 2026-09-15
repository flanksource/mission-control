package mcp

import (
	gocontext "context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/flanksource/duty/context"
	dutyjob "github.com/flanksource/duty/job"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/timberio/go-datemath"
	"gorm.io/gorm"

	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/jobs"
)

const (
	toolListJobs    = "list_jobs"
	toolListJobRuns = "list_job_runs"
	toolGetJobRun   = "get_job_run"

	defaultScheduledJobLimit = 50
	defaultJobRunLimit       = 20
	maximumJobLimit          = 100
)

type jobRunResponse struct {
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

func listJobsHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := requireMonitorRead(ctx); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	limit, err := jobLimit(req, defaultScheduledJobLimit)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	name := strings.ToLower(req.GetString("name", ""))
	resourceType := req.GetString("resource_type", "")
	resourceID := req.GetString("resource_id", "")
	var scheduled []dutyjob.JobCronEntry
	for _, entry := range jobs.FuncScheduler.Entries() {
		job, ok := entry.Job.(*dutyjob.Job)
		if !ok || !matchesScheduledJob(job, name, resourceType, resourceID) {
			continue
		}

		scheduled = append(scheduled, dutyjob.JobCronEntry{
			JobID:        job.ID(),
			Aliases:      job.Aliases,
			ResourceID:   job.ResourceID,
			ResourceType: job.ResourceType,
			Name:         job.Name,
			Schedule:     job.Schedule,
			LastRan:      entry.Prev,
			NextRun:      entry.Next,
			NextRunIn:    time.Until(entry.Next).String(),
		})
	}

	sort.Slice(scheduled, func(i, j int) bool {
		return scheduled[i].JobID < scheduled[j].JobID
	})
	if len(scheduled) > limit {
		scheduled = scheduled[:limit]
	}

	return structToMCPResponse(req, scheduled), nil
}

func listJobRunsHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := requireMonitorRead(ctx); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	limit, err := jobLimit(req, defaultJobRunLimit)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	var histories []models.JobHistory
	err = auth.WithRLS(ctx, func(rlsCtx context.Context) error {
		query := rlsCtx.DB().Order("time_start DESC").Limit(limit)
		if name := req.GetString("name", ""); name != "" {
			query = query.Where("name ILIKE ?", "%"+name+"%")
		}
		if statuses, err := jobStatuses(req.GetStringSlice("status", nil)); err != nil {
			return err
		} else if len(statuses) > 0 {
			query = query.Where("status IN ?", statuses)
		}
		if resourceType := req.GetString("resource_type", ""); resourceType != "" {
			query = query.Where("resource_type = ?", resourceType)
		}
		if resourceID := req.GetString("resource_id", ""); resourceID != "" {
			query = query.Where("resource_id = ?", resourceID)
		}
		if agentID := req.GetString("agent_id", ""); agentID != "" {
			query = query.Where("agent_id = ?", agentID)
		}
		if hasErrors, ok := req.GetArguments()["has_errors"].(bool); ok {
			if hasErrors {
				query = query.Where("error_count > 0")
			} else {
				query = query.Where("error_count = 0")
			}
		}
		if since, err := jobDateFilter(req.GetString("since", "")); err != nil {
			return fmt.Errorf("invalid since: %w", err)
		} else if since != nil {
			query = query.Where("time_start >= ?", *since)
		}
		if before, err := jobDateFilter(req.GetString("before", "")); err != nil {
			return fmt.Errorf("invalid before: %w", err)
		} else if before != nil {
			query = query.Where("time_start <= ?", *before)
		}

		return query.Find(&histories).Error
	})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	runs := make([]jobRunResponse, len(histories))
	for i, history := range histories {
		runs[i] = newJobRunResponse(history, false)
	}
	return structToMCPResponse(req, runs), nil
}

func getJobRunHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := requireMonitorRead(ctx); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	id, err := req.RequireString("id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	var history models.JobHistory
	err = auth.WithRLS(ctx, func(rlsCtx context.Context) error {
		return rlsCtx.DB().Where("id = ?", id).First(&history).Error
	})
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return mcp.NewToolResultError(fmt.Sprintf("job run %s was not found", id)), nil
		}
		return mcp.NewToolResultError(err.Error()), nil
	}

	return structToMCPResponse(req, newJobRunResponse(history, true)), nil
}

func registerJobs(s *server.MCPServer) {
	s.AddTool(mcp.NewTool(toolListJobs,
		mcp.WithDescription("List jobs scheduled in this Mission Control server, including their cron schedule and next run time. Use list_job_runs for execution history."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithString("name", mcp.Description("Optional case-insensitive partial match against the job name or aliases.")),
		mcp.WithString("resource_type", mcp.Description("Optional exact resource type filter.")),
		mcp.WithString("resource_id", mcp.Description("Optional exact resource ID filter.")),
		mcp.WithNumber("limit", mcp.Description("Maximum jobs to return (default: 50, maximum: 100).")),
	), listJobsHandler)

	s.AddTool(mcp.NewTool(toolListJobRuns,
		mcp.WithDescription("List persisted job execution history, ordered by most recent run first. Returns compact diagnostics; use get_job_run for complete details and errors."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithString("name", mcp.Description("Optional case-insensitive partial match against the job name.")),
		mcp.WithArray("status", mcp.WithStringItems(), mcp.Description("Optional statuses: RUNNING, SUCCESS, WARNING, FAILED, STALE, SKIPPED.")),
		mcp.WithString("resource_type", mcp.Description("Optional exact resource type filter.")),
		mcp.WithString("resource_id", mcp.Description("Optional exact resource ID filter.")),
		mcp.WithString("agent_id", mcp.Description("Optional exact agent ID filter.")),
		mcp.WithBoolean("has_errors", mcp.Description("Optional filter for runs with (true) or without (false) errors.")),
		mcp.WithString("since", mcp.Description("Optional inclusive start time in date-math or RFC3339 format, e.g. now-24h.")),
		mcp.WithString("before", mcp.Description("Optional inclusive end time in date-math or RFC3339 format, e.g. now-1h.")),
		mcp.WithNumber("limit", mcp.Description("Maximum runs to return (default: 20, maximum: 100).")),
	), listJobRunsHandler)

	s.AddTool(mcp.NewTool(toolGetJobRun,
		mcp.WithDescription("Get the complete persisted record for a job execution, including its details and errors."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithString("id", mcp.Required(), mcp.Description("Job run ID.")),
	), getJobRunHandler)
}

func requireMonitorRead(ctx context.Context) error {
	owner, err := resolveOwner(ctx)
	if err != nil {
		return err
	}
	allowed, err := rbac.Enforcer().Enforce(owner, policy.ObjectMonitor, policy.ActionRead)
	if err != nil {
		return ctx.Oops().Wrap(err)
	}
	if !allowed {
		return fmt.Errorf("forbidden: user %s does not have monitor:read permission", owner)
	}
	return nil
}

func matchesScheduledJob(job *dutyjob.Job, name, resourceType, resourceID string) bool {
	if name != "" {
		matchesName := strings.Contains(strings.ToLower(job.Name), name)
		matchesAlias := false
		for _, alias := range job.Aliases {
			if strings.Contains(strings.ToLower(alias), name) {
				matchesAlias = true
				break
			}
		}
		if !matchesName && !matchesAlias {
			return false
		}
	}
	return (resourceType == "" || job.ResourceType == resourceType) &&
		(resourceID == "" || job.ResourceID == resourceID)
}

func jobLimit(req mcp.CallToolRequest, defaultLimit int) (int, error) {
	limit := req.GetInt("limit", defaultLimit)
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
		models.StatusRunning: true,
		models.StatusSuccess: true,
		models.StatusWarning: true,
		models.StatusFailed:  true,
		models.StatusStale:   true,
		models.StatusSkipped: true,
	}

	for i, status := range statuses {
		statuses[i] = strings.ToUpper(status)
		if !valid[statuses[i]] {
			return nil, fmt.Errorf("invalid status %q", status)
		}
	}
	return statuses, nil
}

func jobDateFilter(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	expression, err := datemath.Parse(value)
	if err != nil {
		return nil, err
	}
	parsed := expression.Time()
	return &parsed, nil
}

func newJobRunResponse(history models.JobHistory, includeDetails bool) jobRunResponse {
	response := jobRunResponse{
		ID:             history.ID.String(),
		Name:           history.Name,
		Status:         history.Status,
		SuccessCount:   history.SuccessCount,
		ErrorCount:     history.ErrorCount,
		Hostname:       history.Hostname,
		DurationMillis: history.DurationMillis,
		ResourceType:   history.ResourceType,
		ResourceID:     history.ResourceID,
		AgentID:        history.GetAgentID(),
		TimeStart:      history.TimeStart,
		TimeEnd:        history.TimeEnd,
		Errors:         jobRunErrors(history.Details),
	}
	if includeDetails {
		response.Details = history.Details
	}
	return response
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
