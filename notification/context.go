package notification

import (
	"encoding/json"
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/samber/oops"
)

type RecipientType string

const (
	RecipientTypePerson     RecipientType = "person"
	RecipientTypePlaybook   RecipientType = "playbook"
	RecipientTypeTeam       RecipientType = "team"
	RecipientTypeConnection RecipientType = "connection"
	RecipientTypeURL        RecipientType = "url"
	RecipientTypeWebhook    RecipientType = "webhook"
)

type Context struct {
	context.Context
	notificationID uuid.UUID
	recipientType  RecipientType
	log            *models.NotificationSendHistory
	recovery       *recoveryDispatch
	recoveryPolicy *v1.NotificationOnResolved
}

func NewContext(ctx context.Context, notificationID uuid.UUID) *Context {
	return &Context{
		Context:        ctx,
		notificationID: notificationID,
		log:            models.NewNotificationSendHistory(notificationID),
	}
}

func (t Context) WithHistory(h models.NotificationSendHistory) *Context {
	h.WithStartTime(time.Now())
	t.log = &h
	return &t
}

// StartLog records the send as in flight before it's dispatched, so that what dispatch creates
// (playbook runs, delivery receipts) can refer to it.
// A send that failed before is moved back to sending; one that is already in flight is left as is.
func (t *Context) StartLog() error {
	switch t.log.Status {
	case "":
		t.log.Status = models.NotificationStatusSending
		return t.DB().Create(t.log).Error
	case models.NotificationStatusSending:
		return nil
	}

	if err := t.log.Transition(t.DB(), models.NotificationStatusSending, map[string]any{"error": nil}); err != nil {
		return err
	}
	t.log.Status = models.NotificationStatusSending
	t.log.Error = nil
	return nil
}

// EndLog moves the send history to the status its dispatch ended in, and records what the dispatch produced.
func (t *Context) EndLog() error {
	t.log.End()
	return t.log.Transition(t.DB(), t.log.Status, map[string]any{
		"error":           t.log.Error,
		"body":            t.log.Body,
		"body_payload":    t.log.BodyPayload,
		"payload":         t.log.Payload,
		"duration_millis": t.log.DurationMillis,
		"person_id":       t.log.PersonID,
		"team_id":         t.log.TeamID,
		"connection_id":   t.log.ConnectionID,
		"playbook_run_id": t.log.PlaybookRunID,
		"group_id":        t.log.GroupID,
	})
}

func (t *Context) WithMessage(message string) {
	t.log.Body = &message
}

func (t *Context) WithBodyPayload(payload types.JSON) {
	t.log.BodyPayload = payload
}

func (t *Context) WithRecipient(recipientType RecipientType, id *uuid.UUID) {
	t.recipientType = recipientType

	switch recipientType {
	case RecipientTypePerson:
		t.log.PersonID = id
	case RecipientTypePlaybook:
		t.log.PlaybookRunID = id
	case RecipientTypeTeam:
		t.log.TeamID = id
	case RecipientTypeConnection:
		t.log.ConnectionID = id
	case RecipientTypeURL:
		// save nothing
	}
}

func (t *Context) WithError(err error) {
	t.log.Status = models.NotificationStatusError
	if o, ok := oops.AsOops(err); ok {
		oopsErr := map[string]any{
			"error": o.ToMap(),
			"hint":  o.Hint(),
		}

		bb, _ := json.Marshal(oopsErr)
		t.log.Error = lo.ToPtr(string(bb))
	} else {
		t.log.Error = lo.ToPtr(err.Error())
	}
}

func (t *Context) WithSource(event string, resourceID uuid.UUID) {
	t.log.SourceEvent = event
	t.log.ResourceID = resourceID
}

func (t *Context) WithGroupID(groupID *uuid.UUID) {
	t.log.GroupID = groupID
}
