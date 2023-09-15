package notification

import (
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
)

type Context struct {
	*api.Context
	NotificationID string
	log            *models.NotificationSendHistory
}

func NewContext(ctx *api.Context, notificationID string) *Context {
	return &Context{
		Context:        ctx,
		NotificationID: notificationID,
		log:            models.NewNotificationSendHistory(notificationID),
	}
}

func (t *Context) StartLog() error {
	return db.PersistNotificationSendHistory(t.Context, t.log)
}

func (t *Context) EndLog() error {
	return db.PersistNotificationSendHistory(t.Context, t.log.End())
}

func (t *Context) SetMessage(message string) {
	t.log.Body = message
}

func (t *Context) SetError(err string) {
	t.log.Error = &err
}
