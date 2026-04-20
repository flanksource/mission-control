package events

import (
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/postq"
	"github.com/flanksource/duty/postq/pg"
	"github.com/flanksource/incident-commander/utils"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type syncHandlerData struct {
	fn          postq.SyncEventHandlerFunc
	handlerName string
}

type asyncHandlerData struct {
	// name is a stable, human-readable label use for metrics
	name string

	fn           func(ctx context.Context, e models.Events) models.Events
	batchSize    int
	numConsumers int
}

const (
	// eventQueueUpdateChannel is the channel on which new events on the `event_queue` table
	// are notified.
	eventQueueUpdateChannel = "event_queue_updates"

	// record the last `DefaultEventLogSize` events for audit purpose.
	DefaultEventLogSize = 20
)

var SyncHandlers = utils.SyncedMap[string, syncHandlerData]{}
var AsyncHandlers = utils.SyncedMap[string, asyncHandlerData]{}

var consumers []*postq.PGConsumer
var registers []func(ctx context.Context)

func Register(fn func(ctx context.Context)) {
	registers = append(registers, fn)
}

func RegisterAsyncHandler(name string, fn func(ctx context.Context, e models.Events) models.Events, batchSize int, consumers int, events ...string) {
	for _, event := range events {
		AsyncHandlers.Append(event, asyncHandlerData{
			fn:           fn,
			name:         name,
			batchSize:    batchSize,
			numConsumers: consumers,
		})
	}
}

func RegisterSyncHandlerNamed(name string, fn postq.SyncEventHandlerFunc, events ...string) {
	for _, event := range events {
		SyncHandlers.Append(event, syncHandlerData{
			fn:          fn,
			handlerName: name,
		})
	}
}

func ConsumeAll(ctx context.Context) {
	ctx = ctx.WithName("events")
	ctx.Debugf("consuming all events")
	for _, consumer := range consumers {
		consumer.ConsumeUntilEmpty(ctx)
	}
}

// InitConsumers registers event handlers and creates consumers without starting
// background listeners. Use ConsumeAll to process events explicitly.
// This is useful in tests to avoid races between background consumers and test data setup.
func InitConsumers(ctx context.Context) {
	log := ctx.Logger.Named("events")
	for _, fn := range registers {
		fn(ctx)
	}

	SyncHandlers.Each(func(event string, handlers []postq.SyncEventHandlerFunc) {
		consumer := postq.SyncEventConsumer{
			WatchEvents: []string{event},
			Consumers:   handlers,
			ConsumerOption: &postq.ConsumerOption{
				ErrorHandler: defaultLoggerErrorHandler,
			},
		}
		if ec, err := consumer.EventConsumer(); err != nil {
			log.Fatalf("failed to create event consumer: %s", err)
		} else {
			consumers = append(consumers, ec)
		}
	})

	AsyncHandlers.Each(func(event string, handlers []asyncHandlerData) {
		for _, handler := range handlers {
			h := handler.fn
			batchSize := ctx.Properties().Int(event+".batchSize", handler.batchSize)
			consumer := postq.AsyncEventConsumer{
				WatchEvents: []string{event},
				BatchSize:   batchSize,
				Consumer: func(_ctx context.Context, e models.Events) models.Events {
					return h(ctx, e)
				},
				ConsumerOption: &postq.ConsumerOption{
					NumConsumers: handler.numConsumers,
					ErrorHandler: func(ctx context.Context, err error) bool {
						log.Errorf("error consuming event(%s): %v", event, err)
						return false
					},
				},
			}
			if ec, err := consumer.EventConsumer(); err != nil {
				log.Fatalf("failed to create event consumer: %s", err)
			} else {
				consumers = append(consumers, ec)
			}
		}
	})
}

func StartConsumers(ctx context.Context) {
	log := ctx.Logger.Named("events")
	for _, fn := range registers {
		fn(ctx)
	}
	// We listen to all PG Notifications on one channel and distribute it to other consumers
	// based on the events.
	notifyRouter := pg.NewNotifyRouter()
	go notifyRouter.Run(ctx, eventQueueUpdateChannel)

	SyncHandlers.Each(func(event string, handlers []syncHandlerData) {
		log.Tracef("Registering %d sync event handlers for %s", len(handlers), event)

		wrappedHandlers := make([]postq.SyncEventHandlerFunc, 0, len(handlers))
		for _, h := range handlers {
			wrappedHandlers = append(wrappedHandlers, func(ctx context.Context, e models.Event) error {
				start := time.Now()
				err := h.fn(ctx, e)
				success := err == nil
				recordEventHandlerDuration(event, h.handlerName, success, time.Since(start))
				if success {
					recordEventHandlerEvents(event, h.handlerName, 1, 0)
				} else {
					recordEventHandlerEvents(event, h.handlerName, 0, 1)
				}
				return err
			})
		}

		consumer := postq.SyncEventConsumer{
			WatchEvents: []string{event},
			Consumers:   wrappedHandlers,
			ConsumerOption: &postq.ConsumerOption{
				ErrorHandler: defaultLoggerErrorHandler,
			},
		}

		if ec, err := consumer.EventConsumer(); err != nil {
			log.Fatalf("failed to create event consumer: %s", err)
		} else {
			pgsyncNotifyChannel := notifyRouter.GetOrCreateBufferedChannel(0, event)
			consumers = append(consumers, ec)
			go ec.Listen(ctx, pgsyncNotifyChannel)
		}
	})

	AsyncHandlers.Each(func(event string, handlers []asyncHandlerData) {
		log.Tracef("Registering %d async event handlers for %v", len(handlers), event)
		for _, handler := range handlers {
			batchSize := ctx.Properties().Int(event+".batchSize", handler.batchSize)

			consumer := postq.AsyncEventConsumer{
				WatchEvents: []string{event},
				BatchSize:   batchSize,
				Consumer: func(_ctx context.Context, e models.Events) models.Events {
					c := ctx
					if ctx.Properties().Off(event+".trace", false) {
						c = c.WithTrace()
					}
					if ctx.Properties().Off(event+".debug", false) {
						c = c.WithDebug()
					}

					start := time.Now()
					failedEvents := handler.fn(c, e)
					failedCount := len(failedEvents)
					processedCount := len(e) - failedCount
					if processedCount < 0 {
						processedCount = 0
					}

					recordEventHandlerDuration(event, handler.name, failedCount == 0, time.Since(start))
					recordEventHandlerEvents(event, handler.name, processedCount, failedCount)
					return failedEvents
				},
				ConsumerOption: &postq.ConsumerOption{
					NumConsumers: handler.numConsumers,
					ErrorHandler: func(ctx context.Context, err error) bool {
						log.Errorf("error consuming event(%s): %v", event, err)
						return false // don't retry here. Event queue has its own retry mechanism.
					},
				},
			}

			if ec, err := consumer.EventConsumer(); err != nil {
				log.Fatalf("failed to create event consumer: %s", err)
			} else {
				pgasyncNotifyChannel := notifyRouter.GetOrCreateBufferedChannel(handler.numConsumers, event)
				consumers = append(consumers, ec)
				go ec.Listen(ctx, pgasyncNotifyChannel)
			}
		}
	})
}

// on conflict clause when inserting new events to the `event_queue` table
var EventQueueOnConflictClause = clause.OnConflict{
	Columns: models.EventQueueUniqueConstraint(),
	DoUpdates: clause.Assignments(map[string]any{
		"attempts":     0,
		"last_attempt": nil,
		"created_at":   gorm.Expr("NOW()"),
		"error":        clause.Column{Table: "excluded", Name: "error"},
	}),
}

func defaultLoggerErrorHandler(ctx context.Context, err error) bool {
	ctx.Errorf("error consuming: %v", err)
	time.Sleep(time.Second * 5)
	return true
}
