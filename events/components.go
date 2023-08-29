package events

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"gorm.io/gorm"
)

func NewComponentConsumerSync(db *gorm.DB, pgPool *pgxpool.Pool) *EventConsumer {
	return NewEventConsumer(db, pgPool, eventQueueUpdateChannel,
		newEventQueueSyncConsumerFunc(syncConsumerWatchEvents["component"], addNotificationEvent, SavePlaybookRun))
}
