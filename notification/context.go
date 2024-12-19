package notification

import (
	"encoding/json"
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/samber/oops"
)

type RecipientType string

const (
	RecipientTypePerson   RecipientType = "person"
	RecipientTypePlaybook RecipientType = "playbook"
	RecipientTypeTeam     RecipientType = "team"
	RecipientTypeCustom   RecipientType = "custom"
)

type Context struct {
	context.Context
	notificationID uuid.UUID
	recipientType  RecipientType
	log            *models.NotificationSendHistory
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

func (t *Context) StartLog() error {
	return t.DB().Save(t.log).Error
}

func (t *Context) EndLog() error {
	return t.DB().Save(t.log.End()).Error
}

func (t *Context) WithMessage(message string) {
	t.log.Body = &message
}

func (t *Context) WithRecipientType(recipientType RecipientType) {
	t.recipientType = recipientType
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

func (t *Context) WithPersonID(id *uuid.UUID) *Context {
	t.log.PersonID = id
	return t
}

func (t *Context) WithPlaybookRun(id *uuid.UUID) *Context {
	t.log.PlaybookRunID = id
	return t
}
