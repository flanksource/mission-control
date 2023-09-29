package jobs

import (
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty"
	"github.com/flanksource/incident-commander/api"
)

func CleanupNotificationSendHistory(ctx api.Context) error {
	if count, err := duty.DeleteNotificationSendHistory(ctx, 30); err != nil {
		logger.Errorf("Failed to delete notification send history: %v", err)
		return err
	} else if count > 0 {
		logger.Infof("Deleted %d notification send history", count)
	}
	return nil
}
