package events

import (
	"github.com/flanksource/postq"
)

func NewComponentConsumerSync() postq.SyncEventConsumer {
	return postq.SyncEventConsumer{
		WatchEvents: []string{
			EventComponentStatusError,
			EventComponentStatusHealthy,
			EventComponentStatusInfo,
			EventComponentStatusUnhealthy,
			EventComponentStatusWarning,
		},
		Consumers: postq.SyncHandlers(addNotificationEvent, schedulePlaybookRun),
		ConsumerOption: &postq.ConsumerOption{
			ErrorHandler: defaultLoggerErrorHandler,
		},
	}
}
