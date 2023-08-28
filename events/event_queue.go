package events

import (
	"fmt"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/api"
)

// eventMaxAttempts is the maximum number of attempts to process an event in `event_queue`
const eventMaxAttempts = 3

// ProcessBatchFunc processes multiple events and returns the failed ones
type ProcessBatchFunc func(*api.Context, []api.Event) []api.Event

// newEventQueueConsumerFunc returns a new event consumer for the `event_queue` table
// based on the given watch events and process batch function.
func newEventQueueConsumerFunc(watchEvents []string, processBatchFunc ProcessBatchFunc) EventConsumerFunc {
	return func(ctx *api.Context, batchSize int) error {
		tx := ctx.DB().Begin()
		if tx.Error != nil {
			return fmt.Errorf("error initiating db tx: %w", tx.Error)
		}

		const selectEventsQuery = `
			DELETE FROM event_queue
			WHERE id IN (
				SELECT id FROM event_queue
				WHERE 
					attempts <= @maxAttempts AND
					name IN @events AND
					(last_attempt IS NULL OR last_attempt <= NOW() - INTERVAL '1 SECOND' * @baseDelay * POWER(attempts, @exponential))
				ORDER BY priority DESC, created_at ASC
				FOR UPDATE SKIP LOCKED
				LIMIT @batchSize
			)
			RETURNING *
		`

		var events []api.Event
		vals := map[string]any{
			"maxAttempts": eventMaxAttempts,
			"events":      watchEvents,
			"batchSize":   batchSize,
			"baseDelay":   60, // in seconds
			"exponential": 5,  // along with baseDelay = 60, the retries are 1, 6, 31, 156 (in minutes)
		}
		err := tx.Raw(selectEventsQuery, vals).Scan(&events).Error
		if err != nil {
			// Rollback the transaction in case of errors to prevent
			// creating dangling connections and to release the locks
			tx.Rollback()
			return err
		}

		if len(events) == 0 {
			// Commit the transaction in case of no records found to prevent
			// creating dangling connections and to release the locks
			tx.Commit()
			return api.Errorf(api.ENOTFOUND, "No events found")
		}

		failedEvents := processBatchFunc(ctx, events)
		for i := range failedEvents {
			e := &failedEvents[i]
			e.Attempts += 1
			last_attempt := time.Now()
			e.LastAttempt = &last_attempt
			logger.Errorf("Failed to process event[%s]: %s", e.ID, e.Error)
		}

		if len(failedEvents) > 0 {
			if err := tx.Create(failedEvents).Error; err != nil {
				// TODO: More robust way to handle failed event insertion failures
				logger.Errorf("Error inserting into table:event_queue with error:%v. %v", err)
			}
		}

		return tx.Commit().Error
	}
}
