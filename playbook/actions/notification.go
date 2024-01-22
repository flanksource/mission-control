package actions

import (
	"github.com/google/uuid"

	"github.com/flanksource/duty/context"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/notification"
)

// Notification runs the notification action
type Notification struct {
}

func (t *Notification) Run(ctx context.Context, action v1.NotificationAction) error {
	notifContext := notification.NewContext(ctx, uuid.Nil)
	return notification.Send(notifContext, action.Connection, action.URL, action.Title, action.Message, action.Properties)
}
