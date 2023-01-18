package jobs

import (
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/db"
)

func CleanupJobHistoryTable() {
	if err := db.DeleteOldJobHistoryRows(5); err != nil {
		logger.Errorf("Error deleting old job history rows: %v", err)
	}
}
