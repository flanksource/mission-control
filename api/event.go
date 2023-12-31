package api

import (
	"time"

	"github.com/flanksource/duty/types"
	"github.com/flanksource/postq"
	"github.com/google/uuid"
)

// List of all sync events in the `event_queue` table.
//
// These events are generated by the database in response to updates on some of the tables.
const (
	EventCheckPassed = "check.passed"
	EventCheckFailed = "check.failed"

	EventComponentStatusHealthy   = "component.status.healthy"
	EventComponentStatusUnhealthy = "component.status.unhealthy"
	EventComponentStatusInfo      = "component.status.info"
	EventComponentStatusWarning   = "component.status.warning"
	EventComponentStatusError     = "component.status.error"

	EventPlaybookSpecApprovalUpdated = "playbook.spec.approval.updated"

	EventPlaybookApprovalInserted = "playbook.approval.inserted"

	EventIncidentCommentAdded        = "incident.comment.added"
	EventIncidentCreated             = "incident.created"
	EventIncidentDODAdded            = "incident.dod.added"
	EventIncidentDODPassed           = "incident.dod.passed"
	EventIncidentDODRegressed        = "incident.dod.regressed"
	EventIncidentResponderAdded      = "incident.responder.added"
	EventIncidentResponderRemoved    = "incident.responder.removed"
	EventIncidentStatusCancelled     = "incident.status.cancelled"
	EventIncidentStatusClosed        = "incident.status.closed"
	EventIncidentStatusInvestigating = "incident.status.investigating"
	EventIncidentStatusMitigated     = "incident.status.mitigated"
	EventIncidentStatusOpen          = "incident.status.open"
	EventIncidentStatusResolved      = "incident.status.resolved"

	// List of async events.
	//
	// Async events require the handler to talk to 3rd party services.
	// They are not determinant and cannot be reliably rolled back and retried.
	//
	// They are mostly generated by the application itself from sync consumers in response
	// to a sync event.
	// Or, they could also be generated by the database.

	EventPushQueueCreate = "push_queue.create"

	EventNotificationSend = "notification.send"

	EventJiraResponderAdded = "incident.responder.jira.added"
	EventJiraCommentAdded   = "incident.comment.jira.added"

	EventMSPlannerResponderAdded = "incident.responder.msplanner.added"

	EventMSPlannerCommentAdded = "incident.comment.msplanner.added"
)

var (
	EventStatusGroup = []string{
		EventCheckFailed,
		EventCheckPassed,
		EventComponentStatusError,
		EventComponentStatusHealthy,
		EventComponentStatusInfo,
		EventComponentStatusUnhealthy,
		EventComponentStatusWarning,
	}
	EventIncidentGroup = []string{
		EventIncidentCreated,
		EventIncidentDODAdded,
		EventIncidentDODPassed,
		EventIncidentDODRegressed,
		EventIncidentResponderRemoved,
		EventIncidentStatusCancelled,
		EventIncidentStatusClosed,
		EventIncidentStatusInvestigating,
		EventIncidentStatusMitigated,
		EventIncidentStatusOpen,
		EventIncidentStatusResolved,
	}
)

type Event struct {
	ID          uuid.UUID           `gorm:"default:generate_ulid()"`
	Name        string              `json:"name"`
	CreatedAt   time.Time           `json:"created_at"`
	Properties  types.JSONStringMap `json:"properties"`
	Error       *string             `json:"error,omitempty"`
	Attempts    int                 `json:"attempts"`
	LastAttempt *time.Time          `json:"last_attempt"`
	Priority    int                 `json:"priority"`
}

func (t Event) ToPostQEvent() postq.Event {
	return postq.Event{
		ID:          t.ID,
		Name:        t.Name,
		Error:       t.Error,
		Attempts:    t.Attempts,
		LastAttempt: t.LastAttempt,
		Properties:  t.Properties,
		CreatedAt:   t.CreatedAt,
	}
}

// We are using the term `Event` as it represents an event in the
// event_queue table, but the table is named event_queue
// to signify it's usage as a queue
func (Event) TableName() string {
	return "event_queue"
}

type Events []Event

func (events Events) ToPostQEvents() postq.Events {
	var output []postq.Event
	for _, event := range events {
		output = append(output, event.ToPostQEvent())
	}

	return output
}
