package jobs

import (
	"fmt"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/job"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/views"
)

func newPopulateViewsJob(ctx context.Context) *job.Job {
	return &job.Job{
		Context:    ctx,
		Name:       "PopulateViews",
		Schedule:   "@every 15m",
		Singleton:  true,
		JobHistory: true,
		RunNow:     true,
		Retention:  job.RetentionFew,
		Fn: func(ctx job.JobRuntime) error {
			activeViews, err := db.GetAllViews(ctx.Context)
			if err != nil {
				return fmt.Errorf("failed to get all views: %w", err)
			}

			logger.Infof("Processing %d views", len(activeViews))

			for _, view := range activeViews {
				viewResource, err := v1.ViewFromModel(&view)
				if err != nil {
					return fmt.Errorf("failed to convert view model to resource: %v", err)
				}

				if _, err := views.PopulateView(ctx.Context, viewResource); err != nil {
					return fmt.Errorf("failed to populate view %s/%s: %v", view.Namespace, view.Name, err)
				}

				ctx.History.IncrSuccess()
				logger.Debugf("successfully populated view %s/%s", view.Namespace, view.Name)
			}

			return nil
		},
	}
}
