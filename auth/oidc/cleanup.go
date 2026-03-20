package oidc

import (
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/job"
)

func CleanupJob(ctx context.Context) *job.Job {
	storage := &Storage{ctx: ctx}
	return &job.Job{
		Name:     "OIDCCleanup",
		Schedule: "@every 1h",
		Context:  ctx,
		Fn: func(j job.JobRuntime) error {
			return storage.CleanupExpired()
		},
	}
}
