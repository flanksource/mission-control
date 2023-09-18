package notification

import (
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/google/uuid"
)

type Context struct {
	*api.Context
	notificationID uuid.UUID
	log            *models.NotificationSendHistory
}

func NewContext(ctx *api.Context, notificationID uuid.UUID) *Context {
	return &Context{
		Context:        ctx,
		notificationID: notificationID,
		log:            models.NewNotificationSendHistory(notificationID),
	}
}

func (t *Context) StartLog() error {
	return db.PersistNotificationSendHistory(t.Context, t.log)
}

func (t *Context) EndLog() error {
	return db.PersistNotificationSendHistory(t.Context, t.log.End())
}

func (t *Context) LogMessage(message string) {
	t.log.Body = message
}

func (t *Context) LogError(err string) {
	t.log.Error = &err
}

func (t *Context) LogSourceEvent(event string, resourceID uuid.UUID) {
	t.log.SourceEvent = event
	t.log.ResourceID = resourceID
}

func (t *Context) LogPersonID(id *uuid.UUID) {
	t.log.PersonID = id
}
