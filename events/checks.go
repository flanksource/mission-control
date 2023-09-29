package events

import (
	"github.com/flanksource/postq"
)

func NewCheckConsumerSync() postq.SyncEventConsumer {
	return postq.SyncEventConsumer{
		WatchEvents: []string{EventCheckPassed, EventCheckFailed},
		Consumers:   []postq.SyncEventHandlerFunc{SyncAdapter(addNotificationEvent), SyncAdapter(schedulePlaybookRun)},
	}
}
