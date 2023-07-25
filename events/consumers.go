package events

import (
	"context"
	"errors"
	"fmt"
	"github.com/sethvargo/go-retry"
	"sync"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"gorm.io/gorm"
)

type EventConsumer struct {
	WatchEvents []string
	// We process mutliple events and return the failed events
	ProcessBatchFunc func(*api.Context, []api.Event) []*api.Event
	BatchSize        int
	Consumers        int
	DB               *gorm.DB
}

func (e EventConsumer) Validate() error {
	if e.BatchSize <= 0 {
		return fmt.Errorf("BatchSize:%d <= 0", e.BatchSize)
	}
	if e.Consumers <= 0 {
		return fmt.Errorf("Consumers:%d <= 0", e.BatchSize)
	}
	if len(e.WatchEvents) == 0 {
		return fmt.Errorf("No events registered to watch:%d <= 0", len(e.WatchEvents))
	}
	return nil
}

func (t *EventConsumer) consumeEvents() error {
	tx := t.DB.Begin()
	if tx.Error != nil {
		return fmt.Errorf("error initiating db tx: %w", tx.Error)
	}

	ctx := api.NewContext(tx, nil)

	const selectEventsQuery = `
        DELETE FROM event_queue
        WHERE id IN (
            SELECT id FROM event_queue
            WHERE 
                attempts <= @maxAttempts AND
                name IN @events
            ORDER BY priority DESC, created_at ASC
            FOR UPDATE SKIP LOCKED
            LIMIT @batchSize
        )
        RETURNING *
	`

	var events []api.Event
	vals := map[string]any{
		"maxAttempts": eventMaxAttempts,
		"events":      t.WatchEvents,
		"batchSize":   t.BatchSize,
	}
	err := tx.Raw(selectEventsQuery, vals).Scan(&events).Error
	if err != nil {
		// Rollback the transaction in case of no records found to prevent
		// creating dangling connections and to release the locks
		tx.Rollback()
		return err
	}

	if len(events) == 0 {
		return gorm.ErrRecordNotFound
	}

	failedEvents := t.ProcessBatchFunc(ctx, events)
	for _, e := range failedEvents {
		e.Attempts += 1
		last_attempt := time.Now()
		e.LastAttempt = &last_attempt
		logger.Errorf("Failed to process event[%s]: %s", e.ID, e.Error)
	}

	if err := tx.Create(failedEvents).Error; err != nil {
		// TODO: More robust way to handle failed event insertion failures
		logger.Errorf("Error inserting into table:event_queue with error:%v. %v", err)
	}
	return tx.Commit().Error
}

// ConsumeEventsUntilEmpty consumes events forever until the event queue is empty.
func (t *EventConsumer) ConsumeEventsUntilEmpty() {
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

	var wg sync.WaitGroup
	for i := 0; i < t.Consumers; i++ {
		wg.Add(1)
		go consumerFunc(&wg)
	}
	wg.Wait()
}

// listenToPostgresNotify listens to postgres notifications
// and will retry on failure for dbReconnectMaxAttempt times
func (e *EventConsumer) listenToPostgresNotify(pgNotify chan bool) {
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

func (e *EventConsumer) Listen() {
	logger.Infof("Started listening for database notify events")

	if err := e.Validate(); err != nil {
		logger.Fatalf("Error starting event consumer: %v", err)
		return
	}

	// Consume pending events
	e.ConsumeEventsUntilEmpty()

	pgNotify := make(chan bool)
	go e.listenToPostgresNotify(pgNotify)

	for {
		select {
		case <-pgNotify:
			e.ConsumeEventsUntilEmpty()

		case <-time.After(pgNotifyTimeout):
			e.ConsumeEventsUntilEmpty()
		}
	}
}

func StartConsumers(gormDB *gorm.DB, config Config) {
	allConsumers := []EventConsumer{
		NewTeamConsumer(gormDB),
		NewNotificationConsumer(gormDB),
		NewResponderConsumer(gormDB),
	}
	if config.UpstreamPush.Valid() {
		allConsumers = append(allConsumers, NewUpstreamPushConsumer(gormDB, config))
	}

	for _, c := range allConsumers {
		go c.Listen()
	}
}
