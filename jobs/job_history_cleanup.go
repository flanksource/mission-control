package jobs

import (
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
)

func CleanupJobHistoryTable() {
	ctx := api.NewContext(db.Gorm, nil)

	if err := db.DeleteOldJobHistoryRows(ctx, 5); err != nil {
		logger.Errorf("Error deleting old job history rows: %v", err)
	}
}
