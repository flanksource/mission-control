package jobs

import (
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/job"
)

// CleanupEventQueue deletes stale records in the `event_queue` table
func CleanupEventQueue(ctx job.JobRuntime) error {

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
		result := ctx.DB().Exec("DELETE FROM event_queue WHERE name = 'push_queue.create' AND properties->>'table' = ? AND NOW() - created_at > ?", table, age)
		if result.Error != nil {
			ctx.History.AddError(result.Error.Error())
		} else if result.RowsAffected > 0 {
			logger.Warnf("Deleted %d push_queue events for table=%s", result.RowsAffected, table)
			ctx.History.ErrorCount += int(result.RowsAffected)
		}
	}

	defaultAge := time.Hour * 24 * 30
	result := ctx.DB().Exec("DELETE FROM event_queue WHERE name != 'push_queue.create' AND NOW() - created_at > ?", defaultAge)
	if result.Error != nil {
		ctx.History.AddError(result.Error.Error())
	} else if result.RowsAffected > 0 {
		ctx.History.ErrorCount += int(result.RowsAffected)
	}

	return nil
}
