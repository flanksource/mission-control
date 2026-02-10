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
	data := notification.NotificationTemplate{
		Title:      action.Title,
		Message:    action.Message,
		Properties: action.Properties,
	}

	service, err := notification.SendRawNotification(notifContext, action.Connection, action.URL, nil, data, nil)
	if err != nil {
		return nil, err
	}

	output := &NotificationResult{
		Title:   data.Title,
		Message: data.Message,
	}
	if service == "slack" {
		output.Message = ""
		if notification.IsSlackBlocksJSON(data.Message) {
			output.Slack = data.Message
		} else {
			payload := notification.NotificationMessagePayload{
				Title:       data.Title,
				Description: data.Message,
			}
			if slackMsg, err := notification.FormatNotificationMessage(payload, "slack"); err == nil {
				output.Slack = slackMsg
			}
		}
	}

	return output, nil
}
