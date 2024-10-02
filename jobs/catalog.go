package jobs

import (
	"github.com/flanksource/duty/job"
)

var RefreshConfigItemSummary3dView = &job.Job{
	Name:       "RefreshConfigItemSummary3dView",
	Schedule:   "@every 10m",
	Retention:  job.RetentionFew,
	Singleton:  true,
	JobHistory: true,
	RunNow:     true,
	Fn: func(ctx job.JobRuntime) error {
		return job.RefreshConfigItemSummary3d(ctx.Context)
	},
}

var RefreshConfigItemSummary7dView = &job.Job{
	Name:       "RefreshConfigItemSummary7dView",
	Schedule:   "@every 10m",
	Retention:  job.RetentionFew,
	Singleton:  true,
	JobHistory: true,
	RunNow:     true,
	Fn: func(ctx job.JobRuntime) error {
		return job.RefreshConfigItemSummary7d(ctx.Context)
	},
}

var RefreshConfigItemSummary30dView = &job.Job{
	Name:       "RefreshConfigItemSummary30dView",
	Schedule:   "@every 1h",
	Retention:  job.RetentionFew,
	Singleton:  true,
	JobHistory: true,
	RunNow:     true,
	Fn: func(ctx job.JobRuntime) error {
		return job.RefreshConfigItemSummary30d(ctx.Context)
	},
}

var CatalogRefreshJobs = []*job.Job{
	RefreshConfigItemSummary3dView,
	RefreshConfigItemSummary7dView,
	RefreshConfigItemSummary30dView,
}
