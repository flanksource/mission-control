package jobs

import (
	"github.com/flanksource/duty/job"
)

var RefreshCatalogAnaylsisChangeCount7dView = &job.Job{
	Name:       "RefreshCatalogAnaylsisChangeCount7dView",
	Schedule:   "@every 10m",
	Retention:  job.RetentionFew,
	Singleton:  true,
	JobHistory: true,
	RunNow:     true,
	Fn: func(ctx job.JobRuntime) error {
		return job.RefreshConfigItemAnalysisChangeCount7d(ctx.Context)
	},
}

var RefreshCatalogAnaylsisChangeCount30dView = &job.Job{
	Name:       "RefreshCatalogAnaylsisChangeCount30dView",
	Schedule:   "@every 1h",
	Retention:  job.RetentionFew,
	Singleton:  true,
	JobHistory: true,
	RunNow:     true,
	Fn: func(ctx job.JobRuntime) error {
		return job.RefreshConfigItemAnalysisChangeCount30d(ctx.Context)
	},
}
