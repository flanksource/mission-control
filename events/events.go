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
	EventTeamUpdate      = "team.update"
	EventTeamDelete      = "team.delete"
	EventResponderCreate = "responder.create"
	EventNotification    = "notification.create"
	EventCommentCreate   = "comment.create"
	EventPushQueueCreate = "push_queue.create"
)

const (
	eventMaxAttempts      = 3
	waitDurationOnFailure = time.Minute
	pgNotifyTimeout       = time.Minute

	dbReconnectMaxDuration         = time.Minute * 5
	dbReconnectBackoffBaseDuration = time.Second

	MinWorkers uint = 1
)

var (
	NumWorkers uint = 3
)

type Config struct {
	UpstreamConf api.UpstreamConfig
}

type eventHandler struct {
	ctx    context.Context
	gormDB *gorm.DB
	config Config
}

func NewEventHandler(ctx context.Context, gormDB *gorm.DB, config Config) *eventHandler {
	return &eventHandler{
		ctx:    ctx,
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
		err := retry.Do(t.ctx, backoff, func(ctx context.Context) error {
			if err := listen(ctx, pgNotify); err != nil {
				return retry.RetryableError(err)
			}

			return nil
		})

		logger.Errorf("failed to connect to database: %v", err)
	}
}

func (t *eventHandler) consumeEvents() error {
	tx := t.gormDB.WithContext(t.ctx).Begin()
	if tx.Error != nil {
		return fmt.Errorf("error initiating db tx: %w", tx.Error)
	}

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
	case EventResponderCreate:
		err = reconcileResponderEvent(tx, event)
	case EventCommentCreate:
		err = reconcileCommentEvent(tx, event)
	case EventNotification:
		err = publishNotification(tx, event)
	case EventTeamUpdate:
		err = handleTeamUpdate(tx, event)
	case EventTeamDelete:
		err = handleTeamDelete(tx, event)
	case EventPushQueueCreate:
		if upstreamPushEventHandler != nil {
			err = upstreamPushEventHandler.Run(t.ctx, tx, []api.Event{event})
		}
	default:
		logger.Errorf("Unrecognized event name: %s", event.Name)
		return tx.Rollback().Error
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

	if NumWorkers < MinWorkers {
		NumWorkers = MinWorkers
	}

	var wg sync.WaitGroup
	for i := uint(0); i < NumWorkers; i++ {
		wg.Add(1)
		go consumerFunc(&wg)
	}
	wg.Wait()
}
