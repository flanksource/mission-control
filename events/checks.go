package events

import (
	"github.com/flanksource/incident-commander/api"
	"github.com/jackc/pgx/v5/pgxpool"
	"gorm.io/gorm"
)

func NewCheckConsumer(db *gorm.DB, pgPool *pgxpool.Pool) *EventConsumer {
	return NewEventConsumer(db, pgPool, "event_queue_checks",
		newEventQueueConsumerFunc(consumerWatchEvents["check"], processCheckEvents, addNotificationEvent))
}

func processCheckEvents(ctx *api.Context, events []api.Event) []api.Event {
	// No-op
	return nil
}
