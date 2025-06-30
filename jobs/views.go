package jobs

import (
	"fmt"
	"sync"

	"github.com/flanksource/commons/collections"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/job"
	"github.com/flanksource/duty/models"
	"github.com/samber/lo"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/views"
)

const DefaultViewSchedule = "@every 15m"

var viewJobs sync.Map

// newViewJob creates a job for a single view with its own schedule
func newViewJob(ctx context.Context, view *models.View) (*job.Job, error) {
	if view.Spec == nil {
		return nil, fmt.Errorf("view spec is nil")
	}

	viewResource, err := v1.ViewFromModel(view)
	if err != nil {
		return nil, fmt.Errorf("failed to convert view model to resource: %w", err)
	}

	schedule := DefaultViewSchedule
	if viewResource.Spec.Schedule != "" {
		schedule = viewResource.Spec.Schedule
	}

	return &job.Job{
		Context:    ctx,
		Name:       "PopulateView",
		ResourceID: view.ID.String(),
		Schedule:   schedule,
		Singleton:  true,
		JobHistory: true,
		Retention:  job.RetentionFew,
		Fn: func(ctx job.JobRuntime) error {
			result, err := views.PopulateView(ctx.Context, viewResource)
			if err != nil {
				return fmt.Errorf("failed to populate view %s/%s: %w", view.Namespace, view.Name, err)
			}

			ctx.History.SuccessCount = len(result.Rows)
			return nil
		},
	}, nil
}

func syncViewJob(ctx job.JobRuntime, view models.View) error {
	newJob, err := newViewJob(ctx.Context, &view)
	if err != nil {
		return fmt.Errorf("failed to create view job: %w", err)
	}

	var existingJob *job.Job
	if j, ok := viewJobs.Load(view.ID.String()); ok {
		existingJob = j.(*job.Job)
	}

	if existingJob == nil {
		if err := newJob.AddToScheduler(FuncScheduler); err != nil {
			return fmt.Errorf("failed to add view job to scheduler: %w", err)
		}

		viewJobs.Store(view.ID.String(), newJob)
		return nil
	}

	if existingJob.Schedule == newJob.Schedule {
		return nil // do nothing
	}

	existingJob.Unschedule()
	if err := newJob.AddToScheduler(FuncScheduler); err != nil {
		return fmt.Errorf("failed to add view job to scheduler: %w", err)
	}
	viewJobs.Store(view.ID.String(), newJob)

	return nil
}

// syncViewJobs manages individual view jobs - creates new ones, updates existing ones, and removes deleted ones
func syncViewJobs(ctx job.JobRuntime) error {
	activeViews, err := db.GetAllViews(ctx.Context)
	if err != nil {
		return fmt.Errorf("failed to get all views: %w", err)
	}

	activeViewIDs := lo.Map(activeViews, func(view models.View, _ int) string {
		return view.ID.String()
	})

	for _, view := range activeViews {
		if err := syncViewJob(ctx, view); err != nil {
			ctx.History.AddErrorf("failed to sync view job for view %s: %s", view.ID.String(), err)
			continue
		}
	}

	viewJobs.Range(func(_key, value any) bool {
		key := _key.(string)
		if collections.Contains(activeViewIDs, key) {
			return true
		}

		ctx.Logger.V(0).Infof("found a dangling view job: %s", key)
		deleteViewJob(key)
		return true
	})

	return nil
}

// newSyncViewJobsJob creates a job that periodically syncs view jobs
func newSyncViewJobsJob(ctx context.Context) *job.Job {
	return &job.Job{
		Context:    ctx,
		Name:       "SyncViewJobs",
		Schedule:   "@every 5m",
		Singleton:  true,
		JobHistory: true,
		RunNow:     true,
		Retention:  job.RetentionFew,
		Fn: func(ctx job.JobRuntime) error {
			err := syncViewJobs(ctx)
			if err != nil {
				return fmt.Errorf("failed to sync view jobs: %w", err)
			}

			return nil
		},
	}
}

func deleteViewJob(id string) {
	if j, ok := viewJobs.Load(id); ok {
		existingJob := j.(*job.Job)
		existingJob.Unschedule()
		viewJobs.Delete(id)
	}
}
