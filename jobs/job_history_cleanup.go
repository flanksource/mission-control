package jobs

import (
	"github.com/flanksource/duty/job"
)

func CleanupJobHistoryTable(ctx job.JobRuntime) error {
	res := ctx.DB().Exec("DELETE FROM job_history where now() - created_at > interval '30 day'")
	ctx.History.ErrorCount = int(res.RowsAffected)
	return res.Error
}
