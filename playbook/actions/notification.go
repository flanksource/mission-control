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

type NotificationResult struct {
	Title   string `json:"title,omitempty"`
	Message string `json:"message,omitempty"`
	Slack   string `json:"slack,omitempty"`
}

func (t *Notification) Run(ctx context.Context, action v1.NotificationAction) (*NotificationResult, error) {
	notifContext := notification.NewContext(ctx, uuid.Nil)
	payload := notification.NotificationMessagePayload{
		Title:       action.Title,
		Description: action.Message,
	}

	service, err := notification.SendNotification(notifContext, action.Connection, action.URL, payload, action.Properties, nil)
	if err != nil {
		return nil, err
	}

	output := &NotificationResult{
		Title:   payload.Title,
		Message: payload.Description,
	}
	if service == "slack" {
		slackMsg, err := notification.FormatNotificationMessage(payload, "slack")
		if err == nil {
			output.Message = ""
			output.Slack = slackMsg
		}
	}

	return output, nil
}
