package events

import (
	"fmt"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/notification"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func addNotificationEvent(ctx *api.Context, tx *gorm.DB, responder api.Responder) error {
	var notifications []api.Notification
	if err := tx.WithContext(ctx).Where("team_id = ? AND deleted_at IS NULL", responder.TeamID).Find(&notifications).Error; err != nil {
		return err
	}

	for _, n := range notifications {
		if valid, err := notification.IsValid(n.Config.Filter, responder); err != nil {
			return err
		} else if !valid {
			continue
		}

		event := api.Event{
			ID:   uuid.New(),
			Name: EventNotification,
			Properties: map[string]string{
				"notification_id": n.ID.String(),
				"id":              responder.ID.String(),
			},
		}
		if err := tx.Create(event).Error; err != nil {
			return fmt.Errorf("error saving event: %w", err)
		}
	}

	return nil
}

// publishNotification publishes notification aobut addition of new responder to an incident.
func publishNotification(tx *gorm.DB, event api.Event) error {
	responderID := event.Properties["id"]
	notificationID := event.Properties["notification_id"]

	ctx := api.NewContext(tx, nil)

	var _responder api.Responder
	err := tx.Where("id = ? AND external_id is NULL", responderID).Preload("Incident").Preload("Team").Find(&_responder).Error
	if err != nil {
		return err
	}

	var notificationDB api.Notification
	if err := tx.Where("id = ?", notificationID).First(&notificationDB).Error; err != nil {
		return err
	}

	if notificationDB.DeletedAt != nil {
		return nil
	}

	shoutrrr, err := notification.NewShoutrrrClient(ctx, notificationDB)
	if err != nil {
		logger.Errorf("failed to create shoutrrr client: %v", err)
	} else if err := shoutrrr.NotifyResponderAdded(ctx, _responder); err != nil {
		logger.Errorf("failed to notify responder addition: %v", err)
	}

	return nil
}
