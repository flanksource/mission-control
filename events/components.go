package events

import "github.com/flanksource/postq"

func NewComponentConsumerSync() postq.SyncEventConsumer {
	return postq.SyncEventConsumer{
		WatchEvents: []string{
			EventComponentStatusError,
			EventComponentStatusHealthy,
			EventComponentStatusInfo,
			EventComponentStatusUnhealthy,
			EventComponentStatusWarning,
		},
		Consumers: []postq.SyncEventHandlerFunc{SyncAdapter(addNotificationEvent), SyncAdapter(schedulePlaybookRun)},
	}
}
