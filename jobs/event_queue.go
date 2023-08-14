package jobs

import (
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
)

// CleanupEventQueue deletes state records in the `event_queue` table
func CleanupEventQueue() {
	ctx := api.NewContext(db.Gorm, nil)

	jobHistory := models.NewJobHistory("CleanupEventQueue", "", "")
	_ = db.PersistJobHistory(ctx, jobHistory.Start())

	pushQueueSchedule := map[string]time.Duration{
		"topologies":                     time.Hour * 24 * 7,
		"canaries":                       time.Hour * 24 * 7,
		"config_scrapers":                time.Hour * 24 * 7,
		"checks":                         time.Hour * 24 * 7,
		"components":                     time.Hour * 24 * 7,
		"config_items":                   time.Hour * 24 * 7,
		"config_analysis":                time.Hour * 24 * 7,
		"config_changes":                 time.Hour * 24 * 7,
		"config_component_relationships": time.Hour * 24 * 7,
		"component_relationships":        time.Hour * 24 * 7,
		"config_relationships":           time.Hour * 24 * 7,
		"check_statuses":                 time.Hour * 24 * 7,
	}

	for table, age := range pushQueueSchedule {
		result := db.Gorm.Debug().Exec("DELETE FROM event_queue WHERE name = 'push_queue.create' AND properties->>'table' = ? AND NOW() - created_at > ?", table, age)
		if result.Error != nil {
			logger.Errorf("Error cleaning up push_queue events for table=%s: %v", table, result.Error)
			jobHistory.AddError(result.Error.Error())
		} else {
			logger.Debugf("Cleaned up %d push_queue events for table=%s", result.RowsAffected, table)
			jobHistory.SuccessCount = int(result.RowsAffected)
		}
	}

	if err := db.PersistJobHistory(ctx, jobHistory.End()); err != nil {
		logger.Errorf("error persisting job history: %v", err)
	}
}
