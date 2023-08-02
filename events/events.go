package events

import (
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/upstream"
	"gorm.io/gorm"

	"github.com/flanksource/commons/collections/set"
)

const (
	EventTeamUpdate       = "team.update"
	EventTeamDelete       = "team.delete"
	EventNotificationSend = "notification.send"

	EventNotificationUpdate = "notification.update"
	EventNotificationDelete = "notification.delete"

	EventCheckPassed = "check.passed"
	EventCheckFailed = "check.failed"

	EventIncidentCreated             = "incident.created"
	EventIncidentResponderAdded      = "incident.responder.added"
	EventIncidentResponderRemoved    = "incident.responder.removed"
	EventIncidentCommentAdded        = "incident.comment.added"
	EventIncidentDODAdded            = "incident.dod.added"
	EventIncidentDODPassed           = "incident.dod.passed"
	EventIncidentDODRegressed        = "incident.dod.regressed"
	EventIncidentStatusOpen          = "incident.status.open"
	EventIncidentStatusClosed        = "incident.status.closed"
	EventIncidentStatusMitigated     = "incident.status.mitigated"
	EventIncidentStatusResolved      = "incident.status.resolved"
	EventIncidentStatusInvestigating = "incident.status.investigating"
	EventIncidentStatusCancelled     = "incident.status.cancelled"

	EventPushQueueCreate = "push_queue.create"
)

const (
	eventMaxAttempts      = 3
	waitDurationOnFailure = time.Minute
	pgNotifyTimeout       = time.Minute

	dbReconnectMaxDuration         = time.Minute * 5
	dbReconnectBackoffBaseDuration = time.Second
)

type Config struct {
	UpstreamPush upstream.UpstreamConfig
}

func StartConsumers(gormDB *gorm.DB, config Config) {
	allConsumers := []EventConsumer{
		NewTeamConsumer(gormDB),
		NewNotificationConsumer(gormDB),
		NewNotificationSendConsumer(gormDB),
		NewResponderConsumer(gormDB),
	}
	if config.UpstreamPush.Valid() {
		allConsumers = append(allConsumers, NewUpstreamPushConsumer(gormDB, config))
	}

	uniqWatchEvents := set.New[string]()
	for i, c := range allConsumers {
		for _, we := range c.WatchEvents {
			if uniqWatchEvents.Contains(we) {
				logger.Fatalf("Error starting consumers: event[%s] has multiple consumers", we)
			}
		}
		uniqWatchEvents.Add(c.WatchEvents...)
		go allConsumers[i].Listen()
	}
}
