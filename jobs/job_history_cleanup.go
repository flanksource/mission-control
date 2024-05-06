package jobs

import (
	"fmt"
	"time"

	"github.com/flanksource/duty/job"
)

var cleanupStaleJobHistory = &job.Job{
	Name:       "CleanupStaleJobHistory",
	Schedule:   "@every 24h",
	Singleton:  true,
	JobHistory: true,
	Retention:  job.RetentionFew,
	RunNow:     true,
	Fn: func(ctx job.JobRuntime) error {
		staleHistoryMaxAge := ctx.Properties().Duration("job.history.maxAge", time.Hour*24*30)
		count, err := job.CleanupStaleHistory(ctx.Context, staleHistoryMaxAge, "", "")
		if err != nil {
			return fmt.Errorf("error cleaning stale job histories: %w", err)
		}

		runningStaleHistoryMaxAge := ctx.Properties().Duration("job.history.running.maxAge", time.Hour*4)
		runningStale, err := job.CleanupStaleRunningHistory(ctx.Context, runningStaleHistoryMaxAge)
		if err != nil {
			return fmt.Errorf("error cleaning stale RUNNING job histories: %w", err)
		}

		ctx.History.SuccessCount = count + runningStale
		return nil
	},
}
