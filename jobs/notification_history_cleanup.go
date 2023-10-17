package jobs

import (
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/job"
	"github.com/flanksource/incident-commander/db"
)

func CleanupNotificationSendHistory(ctx job.JobRuntime) error {
	if count, err := db.DeleteNotificationSendHistory(ctx.Context, 30); err != nil {
		logger.Errorf("Failed to delete notification send history: %v", err)
		return err
	} else if count > 0 {
		logger.Infof("Deleted %d notification send history", count)
	}
	return nil
}
