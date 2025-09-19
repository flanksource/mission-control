package mcp

import (
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/job"
	"github.com/flanksource/incident-commander/jobs"
	"github.com/mark3labs/mcp-go/server"
)

func SyncViewTools(ctx context.Context, s *server.MCPServer) *job.Job {
	return &job.Job{
		Name:          "SyncMCPViewTools",
		Context:       ctx,
		Schedule:      "@every 1h",
		Singleton:     true,
		JitterDisable: true,
		JobHistory:    true,
		Retention:     job.RetentionFailed,
		RunNow:        true,
		Fn: func(jr job.JobRuntime) error {
			return syncViewsAsTools(ctx, s)
		},
	}
}

func SyncPlaybookTools(ctx context.Context, s *server.MCPServer) *job.Job {
	return &job.Job{
		Name:          "SyncMCPPlaybookTools",
		Context:       ctx,
		Schedule:      "@every 1h",
		Singleton:     true,
		JitterDisable: true,
		JobHistory:    true,
		Retention:     job.RetentionFailed,
		RunNow:        true,
		Fn: func(jr job.JobRuntime) error {
			return syncPlaybooksAsTools(ctx, s)
		},
	}
}

func registerJobs(ctx context.Context, s *server.MCPServer) {
	for _, job := range GetAllJobs(ctx, s) {
		j := job
		j.Context = ctx
		if err := j.AddToScheduler(jobs.FuncScheduler); err != nil {
			logger.Errorf("Failed to schedule %s: %v", j, err)
		}
	}
}

func GetAllJobs(ctx context.Context, s *server.MCPServer) []*job.Job {
	return []*job.Job{
		SyncViewTools(ctx, s),
		SyncPlaybookTools(ctx, s),
	}
}
