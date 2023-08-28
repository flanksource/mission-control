package events

import (
	"fmt"
	"time"

	"github.com/flanksource/commons/collections/set"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/upstream"
	"github.com/flanksource/incident-commander/api"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"gorm.io/gorm"
)

// eventMaxAttempts is the maximum number of attempts to process an event in `event_queue`
const eventMaxAttempts = 3

type (
	// ProcessBatchFunc processes multiple events and returns the failed ones
	ProcessBatchFunc func(*api.Context, []api.Event) []api.Event

	SyncConsumer func(*api.Context, api.Event) error
)

// List of all event types in the `event_queue` table
const (
	EventTeamUpdate       = "team.update"
	EventTeamDelete       = "team.delete"
	EventNotificationSend = "notification.send"

	EventNotificationUpdate = "notification.update"
	EventNotificationDelete = "notification.delete"

	EventCheckPassed = "check.passed"
	EventCheckFailed = "check.failed"

	EventIncidentCreated             = "incident.created"
	EventIncidentResponderAdded      = "incident.responder.added"
	EventIncidentResponderRemoved    = "incident.responder.removed"
	EventIncidentCommentAdded        = "incident.comment.added"
	EventIncidentDODAdded            = "incident.dod.added"
	EventIncidentDODPassed           = "incident.dod.passed"
	EventIncidentDODRegressed        = "incident.dod.regressed"
	EventIncidentStatusOpen          = "incident.status.open"
	EventIncidentStatusClosed        = "incident.status.closed"
	EventIncidentStatusMitigated     = "incident.status.mitigated"
	EventIncidentStatusResolved      = "incident.status.resolved"
	EventIncidentStatusInvestigating = "incident.status.investigating"
	EventIncidentStatusCancelled     = "incident.status.cancelled"

	EventPushQueueCreate = "push_queue.create"
)

type Config struct {
	UpstreamPush upstream.UpstreamConfig
}

// consumerWatchEvents keeps a registry of all the event_queue consumer and the events they watch.
// This helps in ensuring that a single event is not being consumed by multiple consumers.
var consumerWatchEvents = map[string][]string{
	"team":              {EventTeamUpdate, EventTeamDelete},
	"notification_send": {EventNotificationSend},
	"responder":         {EventIncidentResponderAdded, EventIncidentCommentAdded},
	"push_queue":        {EventPushQueueCreate},
	"check":             {EventCheckPassed, EventCheckFailed},
	"notification": {
		EventNotificationUpdate, EventNotificationDelete,
		EventIncidentCreated,
		EventIncidentResponderRemoved,
		EventIncidentDODAdded, EventIncidentDODPassed, EventIncidentDODRegressed,
		EventIncidentStatusOpen, EventIncidentStatusClosed, EventIncidentStatusMitigated,
		EventIncidentStatusResolved, EventIncidentStatusInvestigating, EventIncidentStatusCancelled,
	},
}

func StartConsumers(gormDB *gorm.DB, pgpool *pgxpool.Pool, config Config) {
	uniqWatchEvents := set.New[string]()
	for _, v := range consumerWatchEvents {
		for _, e := range v {
			if uniqWatchEvents.Contains(e) {
				logger.Fatalf("Error starting consumers: event[%s] has multiple consumers", e)
			}

			uniqWatchEvents.Add(e)
		}
	}

	allConsumers := []*EventConsumer{
		NewTeamConsumer(gormDB, pgpool),
		NewCheckConsumer(gormDB, pgpool),
		NewNotificationConsumer(gormDB, pgpool),
		NewNotificationSendConsumer(gormDB, pgpool),
		NewResponderConsumer(gormDB, pgpool),
	}
	if config.UpstreamPush.Valid() {
		allConsumers = append(allConsumers, NewUpstreamPushConsumer(gormDB, pgpool, config))
	}

	for i := range allConsumers {
		go allConsumers[i].Listen()
	}
}

// newEventQueueConsumerFunc returns a new event consumer for the `event_queue` table
// based on the given watch events and process batch function.
func newEventQueueConsumerFunc(watchEvents []string, processBatchFunc ProcessBatchFunc, syncConsumers ...SyncConsumer) EventConsumerFunc {
	return func(ctx *api.Context, batchSize int) error {
		tx := ctx.DB().Begin()
		if tx.Error != nil {
			return fmt.Errorf("error initiating db tx: %w", tx.Error)
		}
		defer tx.Rollback()

		ctx = ctx.WithDB(tx)

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
			return fmt.Errorf("error selecting events: %w", err)
		}

		if len(events) == 0 {
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

		// Process sync consumers
		if len(syncConsumers) != 0 {
			failedEventsIDs := set.New[uuid.UUID]()
			for _, fe := range failedEvents {
				failedEventsIDs.Add(fe.ID)
			}

			for i := range events {
				if failedEventsIDs.Contains(events[i].ID) {
					continue
				}

				for _, syncConsumer := range syncConsumers {
					if err := syncConsumer(ctx, events[i]); err != nil {
						return fmt.Errorf("error processing sync consumer: %w", err)
					}
				}
			}
		}

		return tx.Commit().Error
	}
}
