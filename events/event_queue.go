package events

import (
	"strconv"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/incident-commander/utils"
	"github.com/flanksource/postq"
	"github.com/flanksource/postq/pg"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	// eventQueueUpdateChannel is the channel on which new events on the `event_queue` table
	// are notified.
	eventQueueUpdateChannel = "event_queue_updates"
)

type Handler func(ctx context.Context, e postq.Event) error

var SyncHandlers = utils.SyncedMap[string, func(ctx context.Context, e postq.Event) error]{}
var AsyncHandlers = utils.SyncedMap[string, func(ctx context.Context, e postq.Events) postq.Events]{}

var consumers []*postq.PGConsumer
var registers []func(ctx context.Context)

func Register(fn func(ctx context.Context)) {
	registers = append(registers, fn)
}

func RegisterAsyncHandler(fn func(ctx context.Context, e postq.Events) postq.Events, batchSize int, consumers int, events ...string) {
	for _, event := range events {
		AsyncHandlers.Append(event, fn)
	}
}

func RegisterSyncHandler(fn Handler, events ...string) {
	for _, event := range events {
		SyncHandlers.Append(event, fn)
	}
}

func ConsumeAll(ctx context.Context) {
	ctx.Debugf("consuming all events")
	for _, consumer := range consumers {
		consumer.ConsumeUntilEmpty(ctx)
	}
}

func StartConsumers(ctx context.Context) {
	for _, fn := range registers {
		fn(ctx)
	}
	// We listen to all PG Notifications on one channel and distribute it to other consumers
	// based on the events.
	notifyRouter := pg.NewNotifyRouter()
	go notifyRouter.Run(ctx, eventQueueUpdateChannel)

	properties := ctx.Properties()

	SyncHandlers.Each(func(event string, handlers []func(ctx context.Context, e postq.Event) error) {
		logger.Tracef("Registering %d sync event handlers for %s", len(handlers), event)
		consumer := postq.SyncEventConsumer{
			WatchEvents: []string{event},
			Consumers:   postq.SyncHandlers[context.Context](handlers...),
			ConsumerOption: &postq.ConsumerOption{
				ErrorHandler: defaultLoggerErrorHandler,
			},
		}
		if ec, err := consumer.EventConsumer(); err != nil {
			logger.Fatalf("failed to create event consumer: %s", err)
		} else {
			pgsyncNotifyChannel := notifyRouter.RegisterRoutes(event)
			consumers = append(consumers, ec)
			go ec.Listen(ctx, pgsyncNotifyChannel)
		}
	})

	AsyncHandlers.Each(func(event string, handlers []func(ctx context.Context, e postq.Events) postq.Events) {
		logger.Tracef("Registering %d async event handlers for %v", len(handlers), event)
		for _, handler := range handlers {
			h := handler
			batchSize := 5
			var err error
			if size := properties[event+".batchSize"]; size != "" {
				batchSize, err = strconv.Atoi(size)
				if err != nil {
					logger.Errorf("%s.batchSize of %s is not a number", event, size)
				}
			}

			consumer := postq.AsyncEventConsumer{
				WatchEvents: []string{event},
				BatchSize:   batchSize,
				Consumer: func(_ctx postq.Context, e postq.Events) postq.Events {
					c := ctx
					if trace := properties[event+".trace"]; trace == "true" {
						c = c.WithTrace()
					}
					if debug := properties[event+".debug"]; debug == "true" {
						c = c.WithDebug()
					}
					return h(c, e)
				},
				ConsumerOption: &postq.ConsumerOption{
					ErrorHandler: defaultLoggerErrorHandler,
				},
			}
			if ec, err := consumer.EventConsumer(); err != nil {
				logger.Fatalf("failed to create event consumer: %s", err)
			} else {
				pgasyncNotifyChannel := notifyRouter.RegisterRoutes(event)
				consumers = append(consumers, ec)
				go ec.Listen(ctx, pgasyncNotifyChannel)
			}
		}
	})

}

// on conflict clause when inserting new events to the `event_queue` table
var EventQueueOnConflictClause = clause.OnConflict{
	Columns: []clause.Column{{Name: "name"}, {Name: "properties"}},
	DoUpdates: clause.Assignments(map[string]any{
		"attempts":     0,
		"last_attempt": nil,
		"created_at":   gorm.Expr("NOW()"),
		"error":        clause.Column{Table: "excluded", Name: "error"},
	}),
}

func defaultLoggerErrorHandler(ctx postq.Context, err error) bool {
	logger.Errorf("error consuming: %v", err)
	time.Sleep(time.Second * 5)
	return true
}
