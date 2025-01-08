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
	template := notification.NotificationTemplate{
		Title:      action.Title,
		Message:    action.Message,
		Properties: action.Properties,
	}
	_, err := notification.SendNotification(notifContext, action.Connection, action.URL, nil, template, nil)
	return err
}
