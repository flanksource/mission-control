package events

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/sethvargo/go-retry"
	"gorm.io/gorm"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
)

const (
	EventTeamUpdate = "team.update"
	EventTeamDelete = "team.delete"

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

	EventNotificationSend = "notification.send"
)

const (
	eventMaxAttempts      = 3
	waitDurationOnFailure = time.Minute
	pgNotifyTimeout       = time.Minute

	dbReconnectMaxDuration         = time.Minute * 5
	dbReconnectBackoffBaseDuration = time.Second

	minWorkers uint = 1
)

var (
	NumWorkers uint = 3
)

type Config struct {
	UpstreamConf api.UpstreamConfig
}

type eventHandler struct {
	gormDB *gorm.DB
	config Config
}

func NewEventHandler(gormDB *gorm.DB, config Config) *eventHandler {
	return &eventHandler{
		gormDB: gormDB,
		config: config,
	}
}

func (t *eventHandler) ListenForEvents() {
	logger.Infof("started listening for database notify events")

	// Consume pending events
	t.ConsumeEventsUntilEmpty()

	pgNotify := make(chan bool)
	go t.listenToPostgresNotify(pgNotify)

	for {
		select {
		case <-pgNotify:
			t.ConsumeEventsUntilEmpty()

		case <-time.After(pgNotifyTimeout):
			t.ConsumeEventsUntilEmpty()
		}
	}
}

// listenToPostgresNotify listens to postgres notifications.
// It will retry on failure for dbReconnectMaxAttempt times.
func (t *eventHandler) listenToPostgresNotify(pgNotify chan bool) {
	var listen = func(ctx context.Context, pgNotify chan bool) error {
		conn, err := db.Pool.Acquire(ctx)
		if err != nil {
			return fmt.Errorf("error acquiring database connection: %v", err)
		}
		defer conn.Release()

		_, err = conn.Exec(ctx, "LISTEN event_queue_updates")
		if err != nil {
			return fmt.Errorf("error listening to database notifications: %v", err)
		}
		logger.Infof("listening to database notifications")

		for {
			notification, err := conn.Conn().WaitForNotification(ctx)
			if err != nil {
				return fmt.Errorf("error listening to database notifications: %v", err)
			}

			logger.Tracef("Received database notification: %+v", notification)
			pgNotify <- true
		}
	}

	// retry on failure.
	for {
		backoff := retry.WithMaxDuration(dbReconnectMaxDuration, retry.NewExponential(dbReconnectBackoffBaseDuration))
		err := retry.Do(context.TODO(), backoff, func(ctx context.Context) error {
			if err := listen(ctx, pgNotify); err != nil {
				return retry.RetryableError(err)
			}

			return nil
		})

		logger.Errorf("failed to connect to database: %v", err)
	}
}

func (t *eventHandler) consumeEvents() error {
	tx := t.gormDB.Begin()
	if tx.Error != nil {
		return fmt.Errorf("error initiating db tx: %w", tx.Error)
	}

	ctx := api.NewContext(tx, nil)

	const selectEventsQuery = `
        DELETE FROM event_queue
        WHERE id = (
            SELECT id FROM event_queue
            WHERE 
                attempts <= @maxAttempts
            ORDER BY priority DESC, created_at ASC
            FOR UPDATE SKIP LOCKED
            LIMIT 1
        )
        RETURNING *
	`

	var event api.Event
	err := tx.Raw(selectEventsQuery, map[string]any{"maxAttempts": eventMaxAttempts}).First(&event).Error
	if err != nil {
		// Rollback the transaction in case of no records found to prevent
		// creating dangling connections and to release the locks
		tx.Rollback()
		return err
	}

	var upstreamPushEventHandler *pushToUpstreamEventHandler
	if t.config.UpstreamConf.Valid() {
		upstreamPushEventHandler = newPushToUpstreamEventHandler(t.config.UpstreamConf)
	}

	switch event.Name {
	case EventIncidentResponderAdded:
		err = reconcileResponderEvent(ctx, event)
	case EventIncidentCommentAdded:
		err = reconcileCommentEvent(ctx, event)
	case EventIncidentCreated,
		EventIncidentResponderRemoved,
		EventIncidentDODAdded, EventIncidentDODPassed, EventIncidentDODRegressed,
		EventIncidentStatusOpen, EventIncidentStatusClosed, EventIncidentStatusMitigated, EventIncidentStatusResolved, EventIncidentStatusInvestigating, EventIncidentStatusCancelled,
		EventCheckFailed, EventCheckPassed:
		err = addNotificationEvent(ctx, event)
	case EventNotificationSend:
		err = sendNotification(ctx, event)
	case EventTeamUpdate:
		err = handleTeamUpdate(tx, event)
	case EventTeamDelete:
		err = handleTeamDelete(tx, event)
	case EventNotificationDelete, EventNotificationUpdate:
		err = handleNotificationUpdates(ctx, event)
	case EventPushQueueCreate:
		if upstreamPushEventHandler != nil {
			err = upstreamPushEventHandler.Run(ctx, tx, []api.Event{event})
		}
	default:
		logger.Errorf("Unrecognized event name: %s", event.Name)
		return tx.Commit().Error
	}

	if err != nil {
		logger.Errorf("failed to handle event [%s]: %v", event.Name, err)

		event.Attempts += 1
		event.Error = err.Error()
		last_attempt := time.Now()
		event.LastAttempt = &last_attempt
		if insertErr := tx.Create(&event).Error; insertErr != nil {
			logger.Errorf("Error inserting into table:event_queue with id:%s and error:%v. %v", event.ID, err, insertErr)
			return tx.Rollback().Error
		}
		return tx.Commit().Error
	}

	return tx.Commit().Error
}

// ConsumeEventsUntilEmpty consumes events forever until the event queue is empty.
func (t *eventHandler) ConsumeEventsUntilEmpty() {
	consumerFunc := func(wg *sync.WaitGroup) {
		for {
			err := t.consumeEvents()
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					wg.Done()
					return
				} else {
					logger.Errorf("error processing event, waiting 60s to try again %v", err)
					time.Sleep(waitDurationOnFailure)
				}
			}
		}
	}

	if NumWorkers < minWorkers {
		NumWorkers = minWorkers
	}

	var wg sync.WaitGroup
	for i := uint(0); i < NumWorkers; i++ {
		wg.Add(1)
		go consumerFunc(&wg)
	}
	wg.Wait()
}
