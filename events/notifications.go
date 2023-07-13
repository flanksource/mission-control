package events

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/flanksource/commons/template"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/mail"
	"github.com/flanksource/incident-commander/notification"
	pkgNotification "github.com/flanksource/incident-commander/notification"
	"github.com/flanksource/incident-commander/teams"
	"github.com/flanksource/incident-commander/utils"
	"github.com/google/uuid"
)

type NotificationEventProperties struct {
	ID               string `json:"id"`                          // Resource id. depends what it is based on the original event.
	EventName        string `json:"event_name"`                  // The name of the original event this notification is for.
	PersonID         string `json:"person_id,omitempty"`         // The person recipient.
	TeamID           string `json:"team_id,omitempty"`           // The team recipient.
	NotificationName string `json:"notification_name,omitempty"` // Name of the notification of a team or a custom service of the notification.
	NotificationID   string `json:"notification_id,omitempty"`   // ID of the notification.
}

// NotificationTemplate holds in data for notification
// that'll be used by struct templater.
type NotificationTemplate struct {
	Message    string            `template:"true"`
	Properties map[string]string `template:"true"`
}

func (t *NotificationEventProperties) AsMap() map[string]string {
	m := make(map[string]string)
	b, _ := json.Marshal(&t)
	_ = json.Unmarshal(b, &m)
	return m
}

func (t *NotificationEventProperties) FromMap(m map[string]string) {
	b, _ := json.Marshal(m)
	_ = json.Unmarshal(b, &t)
}

func publishNotification(ctx *api.Context, event api.Event) error {
	var props NotificationEventProperties
	props.FromMap(event.Properties)

	celEnv, err := getEnvForEvent(ctx, props.EventName, event.Properties)
	if err != nil {
		return err
	}

	templater := template.StructTemplater{
		Values:         celEnv,
		ValueFunctions: true,
		DelimSets: []template.Delims{
			{Left: "{{", Right: "}}"},
			{Left: "$(", Right: ")"},
		},
	}

	notification, err := pkgNotification.GetNotification(ctx, props.NotificationID)
	if err != nil {
		return err
	}

	data := NotificationTemplate{
		Message:    notification.Template,
		Properties: notification.Properties,
	}

	if err := templater.Walk(&data); err != nil {
		return fmt.Errorf("error templating notification: %w", err)
	}

	if props.PersonID != "" {
		var emailAddress string
		if err := ctx.DB().Model(&models.Person{}).Select("email").Where("id = ?", props.PersonID).Find(&emailAddress).Error; err != nil {
			return fmt.Errorf("failed to get email of person(id=%s); %v", props.PersonID, err)
		}

		var subject string = "Testing" // TODO: Make this customizable for the user or generate it based on the event?
		email := mail.New(emailAddress, subject, data.Message, "text/html")
		return email.Send()
	}

	if props.TeamID != "" {
		teamSpec, err := teams.GetTeamSpec(ctx, props.TeamID)
		if err != nil {
			return fmt.Errorf("failed to get team(id=%s); %v", props.TeamID, err)
		}

		for _, cn := range teamSpec.Notifications {
			if cn.Name != props.NotificationName {
				continue
			}

			if err := templater.Walk(&cn); err != nil {
				return fmt.Errorf("error templating notification: %w", err)
			}

			return pkgNotification.Publish(ctx, cn.Connection, cn.URL, data.Message, cn.Properties)
		}
	}

	for _, cn := range notification.CustomNotifications {
		if cn.Name != props.NotificationName {
			continue
		}

		if err := templater.Walk(&cn); err != nil {
			return fmt.Errorf("error templating notification: %w", err)
		}

		return pkgNotification.Publish(ctx, cn.Connection, cn.URL, data.Message, cn.Properties)
	}

	return nil
}

// addNotificationEvent responds to a event that can possible generate a notification.
// If a notification is found for the given event and passes all the filters, then
// a new notification event is created.
func addNotificationEvent(ctx *api.Context, event api.Event) error {
	notifications, err := notification.GetNotifications(ctx, event.Name)
	if err != nil {
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

		if valid, err := utils.EvalExpression(n.Filter, celEnv); err != nil {
			return err
		} else if !valid {
			continue
		}

		if n.PersonID != nil {
			prop := NotificationEventProperties{
				EventName:      event.Name,
				NotificationID: n.ID.String(),
				ID:             event.Properties["id"],
				PersonID:       n.PersonID.String(),
			}

			newEvent := api.Event{
				ID:         uuid.New(),
				Name:       EventNotificationPublish,
				Properties: prop.AsMap(),
			}
			if err := ctx.DB().Create(newEvent).Error; err != nil {
				return fmt.Errorf("failed to create notification event for person(id=%s): %v", n.PersonID, err)
			}
		}

		if n.TeamID != nil {
			// TODO: cache team spec
			var team models.Team
			if err := ctx.DB().Model(&models.Team{}).Select("spec").Where("id = ?", n.TeamID).Find(&team).Error; err != nil {
				return fmt.Errorf("failed to get team spec(id=%s); %v", n.TeamID, err)
			}

			b, err := json.Marshal(team.Spec)
			if err != nil {
				return err
			}

			var teamSpec api.TeamSpec
			if err := json.Unmarshal(b, &teamSpec); err != nil {
				return err
			}

			for _, cn := range teamSpec.Notifications {
				if valid, err := utils.EvalExpression(cn.Filter, celEnv); err != nil {
					return err
				} else if !valid {
					continue
				}

				prop := NotificationEventProperties{
					EventName:        event.Name,
					NotificationID:   n.ID.String(),
					ID:               event.Properties["id"],
					TeamID:           n.TeamID.String(),
					NotificationName: cn.Name,
				}

				newEvent := api.Event{
					ID:         uuid.New(),
					Name:       EventNotificationPublish,
					Properties: prop.AsMap(),
				}

				if err := ctx.DB().Create(newEvent).Error; err != nil {
					return fmt.Errorf("failed to create notification event for team(id=%s): %v", n.TeamID, err)
				}
			}
		}

		if n.CustomServices != nil {
			b, err := json.Marshal(n.CustomServices)
			if err != nil {
				return err
			}

			var customNotifications []api.NotificationConfig
			if err := json.Unmarshal(b, &customNotifications); err != nil {
				return err
			}

			for _, cn := range customNotifications {
				if valid, err := utils.EvalExpression(cn.Filter, celEnv); err != nil {
					return err
				} else if !valid {
					continue
				}

				prop := NotificationEventProperties{
					EventName:        event.Name,
					NotificationID:   n.ID.String(),
					ID:               event.Properties["id"],
					NotificationName: cn.Name,
				}

				newEvent := api.Event{
					ID:         uuid.New(),
					Name:       EventNotificationPublish,
					Properties: prop.AsMap(),
				}

				if err := ctx.DB().Create(newEvent).Error; err != nil {
					return fmt.Errorf("failed to create notification event for custom service (id=%s): %v", n.PersonID, err)
				}
			}
		}
	}

	return nil
}

// getEnvForEvent gets the environment variables for the given event
// that'll be passed to the cel expression or to the template renderer as a view.
func getEnvForEvent(ctx *api.Context, eventName string, properties map[string]string) (map[string]any, error) {
	env := make(map[string]any)

	if strings.HasPrefix(eventName, "check.") {
		var check models.Check
		if err := ctx.DB().Where("id = ?", properties["id"]).Find(&check).Error; err != nil {
			return nil, err
		}

		var canary models.Canary
		if err := ctx.DB().Where("id = ?", check.CanaryID).Find(&canary).Error; err != nil {
			return nil, err
		}

		env["canary"] = canary.AsMap()
		env["check"] = check.AsMap()
	}

	if eventName == "incident.created" || strings.HasPrefix(eventName, "incident.status.") {
		var incident models.Incident
		if err := ctx.DB().Where("id = ?", properties["id"]).Find(&incident).Error; err != nil {
			return nil, err
		}

		env["incident"] = incident.AsMap()
	}

	if strings.HasPrefix(eventName, "incident.responder.") {
		var responder models.Responder
		if err := ctx.DB().Where("id = ?", properties["id"]).Find(&responder).Error; err != nil {
			return nil, err
		}

		var incident models.Incident
		if err := ctx.DB().Where("id = ?", responder.IncidentID).Find(&incident).Error; err != nil {
			return nil, err
		}

		env["incident"] = incident.AsMap()
		env["responder"] = responder.AsMap()
	}

	if strings.HasPrefix(eventName, "incident.comment.") {
		var comment models.Comment
		if err := ctx.DB().Where("id = ?", properties["id"]).Find(&comment).Error; err != nil {
			return nil, err
		}

		var incident models.Incident
		if err := ctx.DB().Where("id = ?", comment.IncidentID).Find(&incident).Error; err != nil {
			return nil, err
		}

		// TODO: extract out mentioned users' emails from the comment body

		env["incident"] = incident.AsMap()
		env["comment"] = comment.AsMap()
	}

	if strings.HasPrefix(eventName, "incident.dod.") {
		var evidence models.Evidence
		if err := ctx.DB().Where("id = ?", properties["id"]).Find(&evidence).Error; err != nil {
			return nil, err
		}

		var hypotheses models.Hypothesis
		if err := ctx.DB().Where("id = ?", evidence.HypothesisID).Find(&evidence).Find(&hypotheses).Error; err != nil {
			return nil, err
		}

		var incident models.Incident
		if err := ctx.DB().Where("id = ?", hypotheses.IncidentID).Find(&incident).Error; err != nil {
			return nil, err
		}

		env["evidence"] = evidence.AsMap()
		env["hypotheses"] = hypotheses.AsMap()
		env["incident"] = incident.AsMap()
	}

	return env, nil
}

func handleNotificationUpdates(ctx *api.Context, event api.Event) error {
	if id, ok := event.Properties["id"]; ok {
		notification.PurgeCache(id)
	}

	return nil
}
