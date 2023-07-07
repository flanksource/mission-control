package events

import (
	"fmt"
	"strings"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/notification"
	"github.com/google/uuid"
)

func addNotificationEvent(ctx *api.Context, event api.Event) error {
	// NOTE: might need to cache all the notifications instead of making this request on every event.
	// Then, on notification update event, we update/clear the cache.
	var notifications []models.Notification
	if err := ctx.DB().Debug().Where("deleted_at IS NULL").Where("? = ANY(events)", event.Name).Find(&notifications).Error; err != nil {
		return err
	}

	if len(notifications) == 0 {
		return nil
	}

	celEnv, err := getEnvForEvent(ctx, event.Name, event.Properties)
	if err != nil {
		return err
	}

	for _, n := range notifications {
		if !n.HasRecipients() {
			continue
		}

		if valid, err := notification.IsValid(n.Filter, celEnv); err != nil {
			return err
		} else if !valid {
			continue
		}

		// properties := NotificationEventProperties{
		// 	NotificationID: n.ID.String(),
		// }
		// properties.FromEvent(event)
		// propertiesMap, err := properties.AsMap()
		// if err != nil {
		// 	return err
		// }

		event.Properties["event_name"] = event.Name

		event := api.Event{
			ID:         uuid.New(),
			Name:       EventNotification,
			Properties: event.Properties,
		}
		if err := ctx.DB().Create(event).Error; err != nil {
			return fmt.Errorf("error saving event: %w", err)
		}
	}

	return nil
}

func publishNotification(ctx *api.Context, event api.Event) error {
	var notificationDB models.Notification
	if err := ctx.DB().Where("id = ?", event.Properties["notification_id"]).First(&notificationDB).Error; err != nil {
		return err
	}

	if notificationDB.DeletedAt != nil {
		return nil
	}

	// TODO: Templatize
	// celEnv, err := getCelEnvForEvents(ctx, event.Properties["event_name"], event.Properties)
	// if err != nil {
	// 	return err
	// }

	if notificationDB.PersonID != nil {
		// Send the notification to the email of this person
	}

	if notificationDB.TeamID != nil {
		// Send the notification to all the receivers of this team
	}

	if len(notificationDB.Receivers) > 0 {
		// Send the notification to all the shoutrrr receivers
	}

	// shoutrrr, err := notification.NewShoutrrrClient(ctx, notificationDB)
	// if err != nil {
	// 	logger.Errorf("failed to create shoutrrr client: %v", err)
	// } else if err := shoutrrr.NotifyResponderAdded(ctx, _responder); err != nil {
	// 	logger.Errorf("failed to notify responder addition: %v", err)
	// }

	return nil
}

// getEnvForEvent gets the environment variables for the given event
// that'll be passed to the cel expression or to the template renderer as a view.
func getEnvForEvent(ctx *api.Context, eventName string, properties map[string]string) (map[string]any, error) {
	env := make(map[string]any)

	if strings.HasPrefix(eventName, "incident.") {
		var incident models.Incident
		if err := ctx.DB().Where("id = ?", properties["id"]).Find(&incident).Error; err != nil {
			return nil, err
		}

		env["incident"] = incident.AsMap()
	}

	if strings.HasPrefix(eventName, "incident.responder.") {

	}

	if strings.HasPrefix(eventName, "incident.comment.") {

	}

	if strings.HasPrefix(eventName, "incident.dod.") {

	}

	return env, nil
}
