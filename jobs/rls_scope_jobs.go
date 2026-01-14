package jobs

import (
	"github.com/flanksource/duty/job"
	"github.com/flanksource/duty/models"

	"github.com/flanksource/incident-commander/permission"
)

const (
	ScopeMaterializeAllSchedule = "@every 24h"
	ScopeCleanupDeletedSchedule = "@every 24h"

	materializeAllScopesJobName = "MaterializeAllScopes"
	cleanupDeletedScopesJobName = "CleanupDeletedScopes"
)

var materializeAllScopesJob = &job.Job{
	Name:          materializeAllScopesJobName,
	Schedule:      ScopeMaterializeAllSchedule,
	Singleton:     true,
	JitterDisable: true,
	JobHistory:    true,
	Retention:     job.RetentionFew,
	RunNow:        true, // Must start immediately to materialize existing scopes/permissions. Only needed for initial migration.
	Fn: func(ctx job.JobRuntime) error {
		var scopes []models.Scope
		if err := ctx.DB().Model(&models.Scope{}).
			Where("deleted_at IS NULL").
			Find(&scopes).Error; err != nil {
			return ctx.Oops().Wrapf(err, "failed to list scopes")
		}

		var rebuildCount, errorCount int
		var failed []string
		for _, scope := range scopes {
			jobRun, err := permission.GetProcessScopeJob(ctx.Context, permission.ScopeQueueSourceScope, scope.ID.String(), permission.ScopeQueueActionRebuild)
			if err != nil {
				errorCount++
				failed = append(failed, scope.ID.String())
				continue
			}

			jobRun.Run()
			if jobRun.LastJob != nil {
				if err := jobRun.LastJob.AsError(); err != nil {
					errorCount++
					failed = append(failed, scope.ID.String())
					continue
				}
			}

			rebuildCount++
		}

		ctx.History.SuccessCount = rebuildCount
		ctx.History.ErrorCount = errorCount
		ctx.History.AddDetails("processed", len(scopes))
		ctx.History.AddDetails("rebuild_count", rebuildCount)
		ctx.History.AddDetails("error_count", errorCount)
		if len(failed) > 0 {
			ctx.History.AddDetails("failed_scopes", failed)
		}

		return nil
	},
}

var cleanupDeletedScopesJob = &job.Job{
	Name:          cleanupDeletedScopesJobName,
	Schedule:      ScopeCleanupDeletedSchedule,
	Singleton:     true,
	JitterDisable: true,
	JobHistory:    true,
	Retention:     job.RetentionFew,
	RunNow:        false,
	Fn: func(ctx job.JobRuntime) error {
		var scopes []models.Scope
		if err := ctx.DB().Model(&models.Scope{}).
			Where("deleted_at IS NOT NULL").
			Find(&scopes).Error; err != nil {
			return ctx.Oops().Wrapf(err, "failed to list deleted scopes")
		}

		var removeCount, errorCount int
		var failed []string
		for _, scope := range scopes {
			jobRun, err := permission.GetProcessScopeJob(ctx.Context, permission.ScopeQueueSourceScope, scope.ID.String(), permission.ScopeQueueActionRemove)
			if err != nil {
				errorCount++
				failed = append(failed, scope.ID.String())
				continue
			}

			jobRun.Run()
			if jobRun.LastJob != nil {
				if err := jobRun.LastJob.AsError(); err != nil {
					errorCount++
					failed = append(failed, scope.ID.String())
					continue
				}
			}

			removeCount++
		}

		ctx.History.SuccessCount = removeCount
		ctx.History.ErrorCount = errorCount
		ctx.History.AddDetails("processed", len(scopes))
		ctx.History.AddDetails("remove_count", removeCount)
		ctx.History.AddDetails("error_count", errorCount)
		if len(failed) > 0 {
			ctx.History.AddDetails("failed_scopes", failed)
		}

		return nil
	},
}
