package events

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"gorm.io/gorm"
)

func NewCheckConsumerSync(db *gorm.DB, pgPool *pgxpool.Pool) *EventConsumer {
	return NewEventConsumer(db, pgPool, eventQueueUpdateChannel,
		newEventQueueSyncConsumerFunc(syncConsumerWatchEvents["check"], addNotificationEvent, SavePlaybookRun))
}
