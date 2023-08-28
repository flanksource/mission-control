package events

import (
	"time"

	"github.com/flanksource/duty/upstream"
	"github.com/jackc/pgx/v5/pgxpool"
	"gorm.io/gorm"
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
	waitDurationOnFailure = time.Second * 5
	pgNotifyTimeout       = time.Minute

	dbReconnectMaxDuration         = time.Minute * 5
	dbReconnectBackoffBaseDuration = time.Second
)

type Config struct {
	UpstreamPush upstream.UpstreamConfig
}

func StartConsumers(gormDB *gorm.DB, pgpool *pgxpool.Pool, config Config) {
	allConsumers := []*EventConsumer{
		NewTeamConsumer(gormDB, pgpool),
		NewNotificationConsumer(gormDB, pgpool),
		NewNotificationSendConsumer(gormDB, pgpool),
		NewResponderConsumer(gormDB, pgpool),
	}
	if config.UpstreamPush.Valid() {
		allConsumers = append(allConsumers, NewUpstreamPushConsumer(gormDB, pgpool, config))
	}

	for i := range allConsumers {
		go allConsumers[i].Listen()
	}
}
