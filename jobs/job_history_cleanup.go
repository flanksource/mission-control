package jobs

import (
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/job"
	"github.com/flanksource/incident-commander/db"
)

func CleanupJobHistoryTable(ctx job.JobRuntime) error {
	if err := db.DeleteOldJobHistoryRows(ctx.Context, 3, 10); err != nil {
		logger.Errorf("Error deleting old job history rows: %v", err)
		return err
	}
	return nil
}
