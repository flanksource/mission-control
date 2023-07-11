package events

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/flanksource/commons/collections"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/commons/template"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/notification"
	"github.com/flanksource/incident-commander/utils"
	"github.com/google/uuid"
)

// NotificationTemplate holds in data for notification
// that'll be used by struct templater.
type NotificationTemplate struct {
	Message    string            `template:"true"`
	Properties map[string]string `template:"true"`
}

func addNotificationEvent(ctx *api.Context, event api.Event) error {
	// TODO: need to cache all the notifications instead of making this request on every event.
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
		if !n.HasRecipients() || n.Template == "" {
			continue
		}

		data := &NotificationTemplate{
			Message:    n.Template,
			Properties: n.Properties,
		}

		templater := template.StructTemplater{
			Values:         celEnv,
			ValueFunctions: true,
			DelimSets: []template.Delims{
				{Left: "{{", Right: "}}"},
				{Left: "$(", Right: ")"},
			},
		}
		if err := templater.Walk(data); err != nil {
			return fmt.Errorf("error templating notification: %w", err)
		}

		if valid, err := utils.EvalExpression(n.Filter, celEnv); err != nil {
			return err
		} else if !valid {
			continue
		}

		if n.PersonID != nil {
			var email string
			if err := ctx.DB().Model(&models.Person{}).Select("email").Where("id = ?", n.PersonID).Find(&email).Error; err != nil {
				logger.Errorf("failed to get email of person(id=%s); %v", n.PersonID, err)
			} else {
				// TODO: Put this somewhere else
				// (might need to add new field to notifications table that stores the connection name for the sender email SMTP server)
				var (
					username string
					password string
					host     = "smt.yandex.com"
					port     = 465
				)

				event := api.Event{
					ID:   uuid.New(),
					Name: EventNotification,
					Properties: map[string]string{
						"internal.message": data.Message,
						"internal.url":     fmt.Sprintf("smtp://%s:%s@%s:%d/?auth=Plain&FromAddress=%s&ToAddresses=%s", url.QueryEscape(username), url.QueryEscape(password), host, port, url.QueryEscape(username), url.QueryEscape(email)),
					},
				}

				event.Properties = collections.MergeMap(event.Properties, data.Properties)
				if err := ctx.DB().Create(event).Error; err != nil {
					logger.Errorf("failed to create notification event for person(id=%s): %v", n.PersonID, err)
				}
			}
		}

		if n.TeamID != nil {
			// Get all the team's custom notifications and publish one event per for each of them
		}

		if n.CustomServices != nil {
			// TODO: Publish an event per service
		}
	}

	return nil
}

func publishNotification(ctx *api.Context, event api.Event) error {
	var (
		connection = event.Properties["internal.connection"]
		url        = event.Properties["internal.url"]
		message    = event.Properties["internal.message"]
	)

	delete(event.Properties, "internal.url")
	delete(event.Properties, "internal.message")
	delete(event.Properties, "internal.connection")

	return notification.Publish(ctx, connection, url, message, event.Properties)
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
