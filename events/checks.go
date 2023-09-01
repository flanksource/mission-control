package events

func NewCheckConsumerSync() SyncEventConsumer {
	return SyncEventConsumer{
		watchEvents: []string{EventCheckPassed, EventCheckFailed},
		consumers:   []SyncEventHandlerFunc{addNotificationEvent, schedulePlaybookRun},
	}
}
