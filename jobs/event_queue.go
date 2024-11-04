package jobs

import (
	"time"

	"github.com/flanksource/duty/job"
)

const eventQueueStaleAge = time.Hour * 24 * 30

// CleanupEventQueue deletes stale records in the `event_queue` table
func CleanupEventQueue(ctx job.JobRuntime) error {
	age := ctx.Properties().Duration("event_queue.maxAge", eventQueueStaleAge)
	result := ctx.DB().Exec("DELETE FROM event_queue WHERE NOW() - created_at > ?", age)
	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected > 0 {
		ctx.History.SuccessCount += int(result.RowsAffected)
		ctx.History.SuccessCount += int(result.RowsAffected)
	}

	return nil
}
