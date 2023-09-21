package eventconsumer

import (
	"fmt"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/api"
)

const (
	// waitDurationOnFailure is the duration to wait before attempting to consume events again
	// after an unexpected failure.
	waitDurationOnFailure = time.Second * 5

	defaultPgNotifyTimeout = time.Minute
)

type EventConsumerFunc func(ctx api.Context) (count int, err error)

type EventConsumer struct {
	// Number of concurrent consumers
	numConsumers int

	// pgNotifyTimeout is the timeout to consume events in case no Consume notification is received.
	pgNotifyTimeout time.Duration

	// consumerFunc is responsible in fetching & consuming the events for the given batch size and events.
	// It returns the number of events it fetched.
	consumerFunc EventConsumerFunc
}

// New returns a new EventConsumer
func New(ConsumerFunc EventConsumerFunc) *EventConsumer {
	return &EventConsumer{
		numConsumers:    1,
		consumerFunc:    ConsumerFunc,
		pgNotifyTimeout: defaultPgNotifyTimeout,
	}
}

func (e *EventConsumer) WithNumConsumers(numConsumers int) *EventConsumer {
	e.numConsumers = numConsumers
	return e
}

func (e *EventConsumer) WithNotifyTimeout(timeout time.Duration) *EventConsumer {
	e.pgNotifyTimeout = timeout
	return e
}

func (e EventConsumer) Validate() error {
	if e.numConsumers <= 0 {
		return fmt.Errorf("consumers:%d <= 0", e.numConsumers)
	}
	if e.consumerFunc == nil {
		return fmt.Errorf("consumerFunc is empty")
	}
	return nil
}

// ConsumeEventsUntilEmpty consumes events in a loop until the event queue is empty.
func (t *EventConsumer) ConsumeEventsUntilEmpty(ctx api.Context) {
	for {
		count, err := t.consumerFunc(ctx)
		if err != nil {
			logger.Errorf("error processing event, waiting %s to try again: %v", waitDurationOnFailure, err)
			time.Sleep(waitDurationOnFailure)
		} else if count == 0 {
			return
		}
	}
}

func (e *EventConsumer) Listen(ctx api.Context, pgNotify <-chan string) {
	if err := e.Validate(); err != nil {
		logger.Fatalf("error starting event consumer: %v", err)
		return
	}

	// Consume pending events
	e.ConsumeEventsUntilEmpty(ctx)

	for i := 0; i < e.numConsumers; i++ {
		go func() {
			for {
				select {
				case <-pgNotify:
					e.ConsumeEventsUntilEmpty(ctx)

				case <-time.After(e.pgNotifyTimeout):
					e.ConsumeEventsUntilEmpty(ctx)
				}
			}
		}()
	}
}
