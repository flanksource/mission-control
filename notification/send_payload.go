package notification

import "github.com/flanksource/duty/context"

// SendEventPayload sends a prepared notification payload and records send history.
func SendEventPayload(ctx context.Context, payload NotificationEventPayload) error {
	return sendNotification(ctx, payload)
}
