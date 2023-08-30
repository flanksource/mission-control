package eventconsumer

import (
	"fmt"
	"sync"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/utils"
	"github.com/jackc/pgx/v5/pgxpool"
	"gorm.io/gorm"
)

const (
	// waitDurationOnFailure is the duration to wait before attempting to consume events again
	// after an unexpected failure.
	waitDurationOnFailure = time.Second * 5

	// pgNotifyTimeout is the timeout to consume events in case no Consume notification is received.
	pgNotifyTimeout = time.Minute

	dbReconnectMaxDuration         = time.Minute * 5
	dbReconnectBackoffBaseDuration = time.Second
)

type EventConsumerFunc func(ctx *api.Context, batchSize int) error

type EventConsumer struct {
	db     *gorm.DB
	pgPool *pgxpool.Pool

	// Number of events to process at a time by a single consumer
	batchSize int

	// Number of concurrent consumers
	numConsumers int

	// pgNotifyChannel is the channel to listen to for any new updates on the event queue
	pgNotifyChannel string

	// consumerFunc is responsible in fetching the events for the given batch size and events.
	// It should return a NotFound error if it cannot find any event to consume.
	consumerFunc EventConsumerFunc
}

// New returns a new EventConsumer
func New(DB *gorm.DB, PGPool *pgxpool.Pool, PgNotifyChannel string, ConsumerFunc EventConsumerFunc) *EventConsumer {
	return &EventConsumer{
		batchSize:       1,
		numConsumers:    1,
		db:              DB,
		pgPool:          PGPool,
		pgNotifyChannel: PgNotifyChannel,
		consumerFunc:    ConsumerFunc,
	}
}

func (e *EventConsumer) WithBatchSize(batchSize int) *EventConsumer {
	e.batchSize = batchSize
	return e
}

func (e *EventConsumer) WithNumConsumers(numConsumers int) *EventConsumer {
	e.numConsumers = numConsumers
	return e
}

func (e EventConsumer) Validate() error {
	if e.batchSize <= 0 {
		return fmt.Errorf("BatchSize:%d <= 0", e.batchSize)
	}
	if e.numConsumers <= 0 {
		return fmt.Errorf("consumers:%d <= 0", e.batchSize)
	}
	if e.pgNotifyChannel == "" {
		return fmt.Errorf("pgNotifyChannel is empty")
	}
	if e.consumerFunc == nil {
		return fmt.Errorf("consumerFunc is empty")
	}
	if e.db == nil {
		return fmt.Errorf("DB is nil")
	}
	if e.pgPool == nil {
		return fmt.Errorf("PGPool is nil")
	}
	return nil
}

// ConsumeEventsUntilEmpty consumes events forever until the event queue is empty.
func (t *EventConsumer) ConsumeEventsUntilEmpty(ctx *api.Context) {
	consumerFunc := func(wg *sync.WaitGroup) {
		for {
			err := t.consumerFunc(ctx, t.batchSize)
			if err != nil {
				if api.ErrorCode(err) == api.ENOTFOUND {
					wg.Done()
					return
				}

				logger.Errorf("error processing event, waiting %s to try again: %v", waitDurationOnFailure, err)
				time.Sleep(waitDurationOnFailure)
			}
		}
	}

	var wg sync.WaitGroup
	for i := 0; i < t.numConsumers; i++ {
		wg.Add(1)
		go consumerFunc(&wg)
	}
	wg.Wait()
}

func (e *EventConsumer) Listen() {
	if err := e.Validate(); err != nil {
		logger.Fatalf("error starting event consumer: %v", err)
		return
	}

	ctx := api.NewContext(e.db, nil)

	// Consume pending events
	e.ConsumeEventsUntilEmpty(ctx)

	pgNotify := make(chan string)
	go utils.ListenToPostgresNotify(e.pgPool, e.pgNotifyChannel, dbReconnectMaxDuration, dbReconnectBackoffBaseDuration, pgNotify)

	for {
		select {
		case <-pgNotify:
			e.ConsumeEventsUntilEmpty(ctx)

		case <-time.After(pgNotifyTimeout):
			e.ConsumeEventsUntilEmpty(ctx)
		}
	}
}
