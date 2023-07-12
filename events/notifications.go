package events

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
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

// NotificationTemplate holds in data for notification
// that'll be used by struct templater.
type NotificationTemplate struct {
	Message    string            `template:"true"`
	Properties map[string]string `template:"true"`
}

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

	templater := template.StructTemplater{
		Values:         celEnv,
		ValueFunctions: true,
		DelimSets: []template.Delims{
			{Left: "{{", Right: "}}"},
			{Left: "$(", Right: ")"},
		},
	}

	for _, n := range notifications {
		if !n.HasRecipients() || n.Template == "" {
			continue
		}

		data := NotificationTemplate{
			Message:    n.Template,
			Properties: n.Properties,
		}

		if err := templater.Walk(&data); err != nil {
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
				newEvent := api.Event{
					ID:   uuid.New(),
					Name: EventNotificationPublish,
					Properties: map[string]string{
						"internal.message": data.Message,
						"internal.url": fmt.Sprintf("smtp://%s:%s@%s:%s/?auth=Plain&FromAddress=%s&ToAddresses=%s",
							url.QueryEscape(os.Getenv("SMTP_USER")),
							url.QueryEscape(os.Getenv("SMTP_PASSWORD")),
							os.Getenv("SMTP_HOST"),
							os.Getenv("SMTP_PORT"),
							url.QueryEscape(os.Getenv("SMTP_USER")),
							url.QueryEscape(email),
						),
					},
				}

				newEvent.Properties = collections.MergeMap(newEvent.Properties, data.Properties)
				if err := ctx.DB().Create(newEvent).Error; err != nil {
					logger.Errorf("failed to create notification event for person(id=%s): %v", n.PersonID, err)
				}
			}
		}

		if n.TeamID != nil {
			var team models.Team
			if err := ctx.DB().Model(&models.Team{}).Select("spec").Where("id = ?", n.TeamID).Find(&team).Error; err != nil {
				logger.Errorf("failed to get team spec(id=%s); %v", n.TeamID, err)
			} else {
				b, err := json.Marshal(team.Spec)
				if err != nil {
					return err
				}

				var teamSpec api.TeamSpec
				if err := json.Unmarshal(b, &teamSpec); err != nil {
					return err
				}

				for _, cn := range teamSpec.Notifications {
					if err := templater.Walk(&cn); err != nil {
						return fmt.Errorf("error templating notification: %w", err)
					}

					if valid, err := utils.EvalExpression(cn.Filter, celEnv); err != nil {
						return err
					} else if !valid {
						continue
					}

					newEvent := api.Event{
						ID:   uuid.New(),
						Name: EventNotificationPublish,
						Properties: map[string]string{
							"internal.message":    data.Message,
							"internal.connection": cn.Connection,
							"internal.url":        cn.URL,
						},
					}

					newEvent.Properties = collections.MergeMap(newEvent.Properties, cn.Properties)
					if err := ctx.DB().Create(newEvent).Error; err != nil {
						logger.Errorf("failed to create notification event for team(id=%s): %v", n.TeamID, err)
					}
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
				logger.Infof("%v", err)
			}

			for _, cn := range customNotifications {
				if err := templater.Walk(&cn); err != nil {
					return fmt.Errorf("error templating notification: %w", err)
				}

				if valid, err := utils.EvalExpression(cn.Filter, celEnv); err != nil {
					return err
				} else if !valid {
					continue
				}

				newEvent := api.Event{
					ID:   uuid.New(),
					Name: EventNotificationPublish,
					Properties: map[string]string{
						"internal.message":    data.Message,
						"internal.connection": cn.Connection,
						"internal.url":        cn.URL,
					},
				}

				newEvent.Properties = collections.MergeMap(newEvent.Properties, cn.Properties)
				if err := ctx.DB().Create(newEvent).Error; err != nil {
					logger.Errorf("failed to create notification event for person(id=%s): %v", n.PersonID, err)
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
