package jobs

import (
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
)

func CleanupJobHistoryTable(ctx api.Context) error {
	if err := db.DeleteOldJobHistoryRows(ctx, 3, 10); err != nil {
		logger.Errorf("Error deleting old job history rows: %v", err)
		return err
	}
	return nil
}
