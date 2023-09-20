package notification

import (
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"github.com/google/uuid"
)

type Context struct {
	api.Context
	notificationID uuid.UUID
	log            *models.NotificationSendHistory
}

func NewContext(ctx api.Context, notificationID uuid.UUID) *Context {
	return &Context{
		Context:        ctx,
		notificationID: notificationID,
		log:            models.NewNotificationSendHistory(notificationID),
	}
}

func (t *Context) StartLog() error {
	return t.DB().Save(t.log).Error
}

func (t *Context) EndLog() error {
	return t.DB().Save(t.log.End()).Error
}

func (t *Context) WithMessage(message string) {
	t.log.Body = message
}

func (t *Context) WithError(err string) {
	t.log.Error = &err
}

func (t *Context) WithSource(event string, resourceID uuid.UUID) {
	t.log.SourceEvent = event
	t.log.ResourceID = resourceID
}

func (t *Context) WithPersonID(id *uuid.UUID) {
	t.log.PersonID = id
}
