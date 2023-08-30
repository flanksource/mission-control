package events

import (
	"github.com/flanksource/incident-commander/events/eventconsumer"
	"github.com/jackc/pgx/v5/pgxpool"
	"gorm.io/gorm"
)

func NewCheckConsumerSync(db *gorm.DB, pgPool *pgxpool.Pool) *eventconsumer.EventConsumer {
	return eventconsumer.New(db, pgPool, eventQueueUpdateChannel,
		newEventQueueSyncConsumerFunc(syncConsumerWatchEvents["check"], addNotificationEvent, SavePlaybookRun))
}
