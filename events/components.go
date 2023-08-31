package events

func NewComponentConsumerSync() SyncEventConsumer {
	return SyncEventConsumer{
		watchEvents: []string{
			EventComponentStatusError,
			EventComponentStatusHealthy,
			EventComponentStatusInfo,
			EventComponentStatusUnhealthy,
			EventComponentStatusWarning,
		},
		consumers: []SyncEventHandlerFunc{addNotificationEvent, SavePlaybookRun},
	}
}
