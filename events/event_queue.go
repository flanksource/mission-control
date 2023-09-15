package events

import (
	"fmt"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/upstream"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/events/eventconsumer"
	"github.com/jackc/pgx/v5/pgxpool"
	"gorm.io/gorm"
)

const (
	// eventMaxAttempts is the maximum number of attempts to process an event in `event_queue`
	eventMaxAttempts = 3

	// eventQueueUpdateChannel is the channel on which new events on the `event_queue` table
	// are notified.
	eventQueueUpdateChannel = "event_queue_updates"
)

type (
	// AsyncEventHandlerFunc processes multiple events and returns the failed ones
	AsyncEventHandlerFunc func(*api.Context, []api.Event) []api.Event

	// SyncEventHandlerFunc processes a single event and ONLY makes db changes.
	SyncEventHandlerFunc func(*api.Context, api.Event) error
)

// List of all sync events in the `event_queue` table.
//
// These events are generated by the database in response to updates on some of the tables.
const (
	EventTeamUpdate = "team.update"
	EventTeamDelete = "team.delete"

	EventCheckPassed = "check.passed"
	EventCheckFailed = "check.failed"

	EventComponentStatusHealthy   = "component.status.healthy"
	EventComponentStatusUnhealthy = "component.status.unhealthy"
	EventComponentStatusInfo      = "component.status.info"
	EventComponentStatusWarning   = "component.status.warning"
	EventComponentStatusError     = "component.status.error"

	EventNotificationUpdate = "notification.update"
	EventNotificationDelete = "notification.delete"

	EventPlaybookSpecApprovalUpdated = "playbook.spec.approval.updated"

	EventPlaybookApprovalInserted = "playbook.approval.inserted"

	EventIncidentCommentAdded        = "incident.comment.added"
	EventIncidentCreated             = "incident.created"
	EventIncidentDODAdded            = "incident.dod.added"
	EventIncidentDODPassed           = "incident.dod.passed"
	EventIncidentDODRegressed        = "incident.dod.regressed"
	EventIncidentResponderAdded      = "incident.responder.added"
	EventIncidentResponderRemoved    = "incident.responder.removed"
	EventIncidentStatusCancelled     = "incident.status.cancelled"
	EventIncidentStatusClosed        = "incident.status.closed"
	EventIncidentStatusInvestigating = "incident.status.investigating"
	EventIncidentStatusMitigated     = "incident.status.mitigated"
	EventIncidentStatusOpen          = "incident.status.open"
	EventIncidentStatusResolved      = "incident.status.resolved"
)

// List of async events.
//
// Async events require the handler to talk to 3rd party services.
// They are not determinant and cannot be reliably rolled back and retried.
//
// They are mostly generated by the application itself from sync consumers in response
// to a sync event.
// Or, they could also be generated by the database.
const (
	EventPushQueueCreate = "push_queue.create"

	EventNotificationSend = "notification.send"

	EventJiraResponderAdded = "incident.responder.jira.added"
	EventJiraCommentAdded   = "incident.comment.jira.added"

	EventMSPlannerResponderAdded = "incident.responder.msplanner.added"
	EventMSPlannerCommentAdded   = "incident.comment.msplanner.added"
)

type Config struct {
	UpstreamPush upstream.UpstreamConfig
}

func StartConsumers(gormDB *gorm.DB, pgpool *pgxpool.Pool, config Config) {
	uniqEvents := make(map[string]struct{})
	allSyncHandlers := []SyncEventConsumer{
		NewTeamConsumerSync(),
		NewCheckConsumerSync(),
		NewComponentConsumerSync(),
		NewResponderConsumerSync(),
		NewCommentConsumerSync(),
		NewNotificationSaveConsumerSync(),
		NewNotificationUpdatesConsumerSync(),
		NewPlaybookApprovalConsumerSync(),
		NewPlaybookApprovalSpecUpdatedConsumerSync(),
	}

	for i := range allSyncHandlers {
		for _, event := range allSyncHandlers[i].watchEvents {
			if _, ok := uniqEvents[event]; ok {
				logger.Fatalf("Watch event %s is duplicated", event)
			}

			uniqEvents[event] = struct{}{}
		}

		go allSyncHandlers[i].EventConsumer(gormDB, pgpool).Listen()
	}

	asyncConsumers := []AsyncEventConsumer{
		NewNotificationSendConsumerAsync(),
		NewResponderConsumerAsync(),
	}
	if config.UpstreamPush.Valid() {
		asyncConsumers = append(asyncConsumers, NewUpstreamPushConsumerAsync(config))
	}

	for i := range asyncConsumers {
		for _, event := range asyncConsumers[i].watchEvents {
			if _, ok := uniqEvents[event]; ok {
				logger.Fatalf("Watch event %s is duplicated", event)
			}

			uniqEvents[event] = struct{}{}
		}

		go asyncConsumers[i].EventConsumer(gormDB, pgpool).Listen()
	}
}

// fetchEvents fetches given watch events from the `event_queue` table.
func fetchEvents(ctx *api.Context, watchEvents []string, batchSize int) ([]api.Event, error) {
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
	err := ctx.DB().Raw(selectEventsQuery, vals).Scan(&events).Error
	if err != nil {
		return nil, fmt.Errorf("error selecting events: %w", err)
	}

	return events, nil
}

type SyncEventConsumer struct {
	watchEvents  []string
	consumers    []SyncEventHandlerFunc
	numConsumers int
}

func (t SyncEventConsumer) EventConsumer(db *gorm.DB, pool *pgxpool.Pool) *eventconsumer.EventConsumer {
	consumer := eventconsumer.New(db, pool, eventQueueUpdateChannel, t.Handle)
	if t.numConsumers > 0 {
		consumer = consumer.WithNumConsumers(t.numConsumers)
	}

	return consumer
}

func (t *SyncEventConsumer) Handle(ctx *api.Context) error {
	tx := ctx.DB().Begin()
	if tx.Error != nil {
		return fmt.Errorf("error initiating db tx: %w", tx.Error)
	}
	defer tx.Rollback()

	ctx = ctx.WithDB(tx)

	events, err := fetchEvents(ctx, t.watchEvents, 1)
	if err != nil {
		return fmt.Errorf("error fetching events: %w", err)
	}

	if len(events) == 0 {
		return api.Errorf(api.ENOTFOUND, "No events found")
	}

	for _, syncConsumer := range t.consumers {
		if err := syncConsumer(ctx, events[0]); err != nil {
			return fmt.Errorf("error processing sync consumer: %w", err)
		}
	}

	// FIXME: When this fails we only roll it back and the attempt is never increased.
	// Also, error is never saved.
	return tx.Commit().Error
}

type AsyncEventConsumer struct {
	watchEvents  []string
	batchSize    int
	consumer     AsyncEventHandlerFunc
	numConsumers int
}

func (t *AsyncEventConsumer) Handle(ctx *api.Context) error {
	tx := ctx.DB().Begin()
	if tx.Error != nil {
		return fmt.Errorf("error initiating db tx: %w", tx.Error)
	}
	defer tx.Rollback()

	ctx = ctx.WithDB(tx)

	events, err := fetchEvents(ctx, t.watchEvents, t.batchSize)
	if err != nil {
		return fmt.Errorf("error fetching events: %w", err)
	}

	if len(events) == 0 {
		return api.Errorf(api.ENOTFOUND, "No events found")
	}

	failedEvents := t.consumer(ctx, events)
	lastAttempt := time.Now()

	for i := range failedEvents {
		e := &failedEvents[i]
		e.Attempts += 1
		e.LastAttempt = &lastAttempt
		logger.Errorf("Failed to process event[%s]: %s", e.ID, e.Error)
	}

	if len(failedEvents) > 0 {
		if err := tx.Create(failedEvents).Error; err != nil {
			// TODO: More robust way to handle failed event insertion failures
			logger.Errorf("Error inserting into table:event_queue with error: %v", err)
		}
	}

	return tx.Commit().Error
}

func (t AsyncEventConsumer) EventConsumer(db *gorm.DB, pool *pgxpool.Pool) *eventconsumer.EventConsumer {
	consumer := eventconsumer.New(db, pool, eventQueueUpdateChannel, t.Handle)
	if t.numConsumers > 0 {
		consumer = consumer.WithNumConsumers(t.numConsumers)
	}

	return consumer
}
