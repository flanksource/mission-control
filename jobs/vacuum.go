package jobs

import "github.com/flanksource/duty/job"

var VacuumTables = &job.Job{
	Name:       "VacuumTables",
	Schedule:   "@every 24h",
	Singleton:  true,
	JobHistory: true,
	Retention:  job.RetentionFew,
	Fn: func(ctx job.JobRuntime) error {
		return ctx.DB().Exec("VACUUM FULL ANALYZE config_items").Error
	},
}
