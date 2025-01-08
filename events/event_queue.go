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

// events received on this channel are saved to DB.
// NOTE: Not sure about this one. will probably remove it.
var EventChan = make(chan models.Event)

type asyncHandlerData struct {
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

var SyncHandlers = utils.SyncedMap[string, postq.SyncEventHandlerFunc]{}
var AsyncHandlers = utils.SyncedMap[string, asyncHandlerData]{}

var consumers []*postq.PGConsumer
var registers []func(ctx context.Context)

func Register(fn func(ctx context.Context)) {
	registers = append(registers, fn)
}

func RegisterAsyncHandler(fn func(ctx context.Context, e models.Events) models.Events, batchSize int, consumers int, events ...string) {
	for _, event := range events {
		AsyncHandlers.Append(event, asyncHandlerData{
			fn:           fn,
			batchSize:    batchSize,
			numConsumers: consumers,
		})
	}
}

func RegisterSyncHandler(fn postq.SyncEventHandlerFunc, events ...string) {
	for _, event := range events {
		SyncHandlers.Append(event, fn)
	}
}

func ConsumeAll(ctx context.Context) {
	ctx = ctx.WithName("events")
	ctx.Debugf("consuming all events")
	for _, consumer := range consumers {
		consumer.ConsumeUntilEmpty(ctx)
	}

}

func StartConsumers(ctx context.Context) {
	log := ctx.Logger.Named("events")
	for _, fn := range registers {
		fn(ctx)
	}

	go func() {
		for e := range EventChan {
			if err := ctx.DB().Create(&e).Error; err != nil {
				log.Errorf("failed to create event: %w", err)
			}
		}
	}()

	// We listen to all PG Notifications on one channel and distribute it to other consumers
	// based on the events.
	notifyRouter := pg.NewNotifyRouter()
	go notifyRouter.Run(ctx, eventQueueUpdateChannel)

	SyncHandlers.Each(func(event string, handlers []postq.SyncEventHandlerFunc) {
		log.Tracef("Registering %d sync event handlers for %s", len(handlers), event)
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
			pgsyncNotifyChannel := notifyRouter.GetOrCreateBufferedChannel(0, event)
			consumers = append(consumers, ec)
			go ec.Listen(ctx, pgsyncNotifyChannel)
		}
	})

	AsyncHandlers.Each(func(event string, handlers []asyncHandlerData) {
		log.Tracef("Registering %d async event handlers for %v", len(handlers), event)
		for _, handler := range handlers {
			h := handler.fn
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
					return h(c, e)
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
	Columns: []clause.Column{{Name: "name"}, {Name: "properties"}},
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
