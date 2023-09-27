package events

import (
	"fmt"
	"strings"

	"github.com/flanksource/duty"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/logs"
	"github.com/flanksource/incident-commander/notification"
	pkgResponder "github.com/flanksource/incident-commander/responder"
	"github.com/flanksource/incident-commander/teams"
	"github.com/flanksource/incident-commander/utils/expression"
	"github.com/google/uuid"
)

// List of all possible variables for any expression related to notifications
var allEnvVars = []string{"check", "canary", "incident", "team", "responder", "comment", "evidence", "hypothesis"}

func NewNotificationSaveConsumerSync() SyncEventConsumer {
	return SyncEventConsumer{
		watchEvents: []string{
			EventIncidentCreated,
			EventIncidentDODAdded,
			EventIncidentDODPassed,
			EventIncidentDODRegressed,
			EventIncidentResponderRemoved,
			EventIncidentStatusCancelled,
			EventIncidentStatusClosed,
			EventIncidentStatusInvestigating,
			EventIncidentStatusMitigated,
			EventIncidentStatusOpen,
			EventIncidentStatusResolved,
		},
		numConsumers: 3,
		consumers: []SyncEventHandlerFunc{
			addNotificationEvent,
		},
	}
}

func NewNotificationSendConsumerAsync() AsyncEventConsumer {
	return AsyncEventConsumer{
		watchEvents:  []string{EventNotificationSend},
		consumer:     sendNotifications,
		batchSize:    1,
		numConsumers: 5,
	}
}

// addNotificationEvent responds to a event that can possibly generate a notification.
// If a notification is found for the given event and passes all the filters, then
// a new `notification.send` event is created.
func addNotificationEvent(ctx api.Context, event api.Event) error {
	notificationIDs, err := notification.GetNotificationIDsForEvent(ctx, event.Name)
	if err != nil {
		return err
	}

	if len(notificationIDs) == 0 {
		return nil
	}

	celEnv, err := getEnvForEvent(ctx, event, event.Properties)
	if err != nil {
		return err
	}

	for _, id := range notificationIDs {
		n, err := notification.GetNotification(ctx, id)
		if err != nil {
			return err
		}

		if !n.HasRecipients() {
			continue
		}

		if n.Error != nil {
			// A notification that currently has errors is skipped.
			continue
		}

		if valid, err := expression.Eval(n.Filter, celEnv, allEnvVars); err != nil {
			logs.IfError(db.UpdateNotificationError(id, err.Error()), "failed to update notification")
		} else if !valid {
			continue
		}

		resourceID, err := uuid.Parse(event.Properties["id"])
		if err != nil {
			return fmt.Errorf("failed to parse resource id: %v", err)
		}

		if n.PersonID != nil {
			payload := notification.NotificationEventPayload{
				EventName:      event.Name,
				NotificationID: n.ID,
				ID:             resourceID,
				PersonID:       n.PersonID,
				EventCreatedAt: event.CreatedAt,
			}

			newEvent := &api.Event{
				Name:       EventNotificationSend,
				Properties: payload.AsMap(),
			}
			if err := ctx.DB().Clauses(eventQueueOnConflictClause).Create(newEvent).Error; err != nil {
				return fmt.Errorf("failed to create notification event for person(id=%s): %v", n.PersonID, err)
			}
		}

		if n.TeamID != nil {
			teamSpec, err := teams.GetTeamSpec(ctx, n.TeamID.String())
			if err != nil {
				return fmt.Errorf("failed to get team(id=%s); %v", n.TeamID, err)
			}

			for _, cn := range teamSpec.Notifications {
				if valid, err := expression.Eval(cn.Filter, celEnv, allEnvVars); err != nil {
					logs.IfError(db.UpdateNotificationError(id, err.Error()), "failed to update notification")
				} else if !valid {
					continue
				}

				payload := notification.NotificationEventPayload{
					EventName:        event.Name,
					NotificationID:   n.ID,
					ID:               resourceID,
					TeamID:           n.TeamID.String(),
					NotificationName: cn.Name,
					EventCreatedAt:   event.CreatedAt,
				}

				newEvent := &api.Event{
					Name:       EventNotificationSend,
					Properties: payload.AsMap(),
				}

				if err := ctx.DB().Clauses(eventQueueOnConflictClause).Create(newEvent).Error; err != nil {
					return fmt.Errorf("failed to create notification event for team(id=%s): %v", n.TeamID, err)
				}
			}
		}

		for _, cn := range n.CustomNotifications {
			if valid, err := expression.Eval(cn.Filter, celEnv, allEnvVars); err != nil {
				logs.IfError(db.UpdateNotificationError(id, err.Error()), "failed to update notification")
			} else if !valid {
				continue
			}

			payload := notification.NotificationEventPayload{
				EventName:        event.Name,
				NotificationID:   n.ID,
				ID:               resourceID,
				NotificationName: cn.Name,
				EventCreatedAt:   event.CreatedAt,
			}

			newEvent := &api.Event{
				Name:       EventNotificationSend,
				Properties: payload.AsMap(),
			}

			if err := ctx.DB().Clauses(eventQueueOnConflictClause).Create(newEvent).Error; err != nil {
				return fmt.Errorf("failed to create notification event for custom service (name:%s): %v", cn.Name, err)
			}
		}
	}

	return nil
}

// sendNotifications sends a notification for each of the given events - one at a time.
// It returns any events that failed to send.
func sendNotifications(ctx api.Context, events []api.Event) []api.Event {
	var failedEvents []api.Event
	for _, e := range events {
		var payload notification.NotificationEventPayload
		payload.FromMap(e.Properties)

		notificationContext := notification.NewContext(ctx, payload.NotificationID)
		notificationContext.WithSource(payload.EventName, payload.ID)
		logs.IfError(notificationContext.StartLog(), "error persisting start of notification send history")

		originalEvent := api.Event{Name: payload.EventName, CreatedAt: payload.EventCreatedAt}
		celEnv, err := getEnvForEvent(ctx, originalEvent, e.Properties)
		if err != nil {
			e.Error = err.Error()
			failedEvents = append(failedEvents, e)
			notificationContext.WithError(err.Error())
			logs.IfError(notificationContext.EndLog(), "error persisting end of notification send history")
		}

		if err := notification.SendNotification(notificationContext, payload, celEnv); err != nil {
			e.Error = err.Error()
			failedEvents = append(failedEvents, e)
			notificationContext.WithError(err.Error())
		}

		logs.IfError(notificationContext.EndLog(), "error persisting end of notification send history")
	}

	return failedEvents
}

// getEnvForEvent gets the environment variables for the given event
// that'll be passed to the cel expression or to the template renderer as a view.
func getEnvForEvent(ctx api.Context, event api.Event, properties map[string]string) (map[string]any, error) {
	env := make(map[string]any)

	if strings.HasPrefix(event.Name, "check.") {
		checkID := properties["id"]

		check, err := duty.FindCachedCheck(ctx, checkID)
		if err != nil {
			return nil, fmt.Errorf("error finding check: %v", err)
		} else if check == nil {
			return nil, fmt.Errorf("check(id=%s) not found", checkID)
		}

		canary, err := duty.FindCachedCanary(ctx, check.CanaryID.String())
		if err != nil {
			return nil, fmt.Errorf("error finding canary: %v", err)
		} else if canary == nil {
			return nil, fmt.Errorf("canary(id=%s) not found", check.CanaryID)
		}

		agent, err := duty.FindCachedAgent(ctx, check.AgentID.String())
		if err != nil {
			return nil, fmt.Errorf("error finding agent: %v", err)
		} else if agent != nil {
			env["agent"] = agent.AsMap()
		}

		summary, err := duty.CheckSummary(ctx, checkID)
		if err != nil {
			return nil, fmt.Errorf("failed to get check summary: %w", err)
		} else if summary != nil {
			check.Uptime = summary.Uptime
			check.Latency = summary.Latency
		}

		// We fetch the latest check_status at the time of event creation
		var checkStatus models.CheckStatus
		if err := ctx.DB().Where("check_id = ?", checkID).Where("created_at >= ?", event.CreatedAt).Order("created_at").First(&checkStatus).Error; err != nil {
			return nil, fmt.Errorf("failed to get check status: %w", err)
		}

		env["status"] = checkStatus.AsMap()
		env["canary"] = canary.AsMap("spec")
		env["check"] = check.AsMap("spec")
		env["permalink"] = fmt.Sprintf("%s/health?layout=table&checkId=%s&timeRange=1h", api.PublicWebURL, check.ID)
	}

	if event.Name == "incident.created" || strings.HasPrefix(event.Name, "incident.status.") {
		incidentID := properties["id"]

		incident, err := duty.FindCachedIncident(ctx, incidentID)
		if err != nil {
			return nil, fmt.Errorf("error finding incident(id=%s): %v", incidentID, err)
		} else if incident == nil {
			return nil, fmt.Errorf("incident(id=%s) not found", incidentID)
		}

		env["incident"] = incident.AsMap()
		env["permalink"] = fmt.Sprintf("%s/incidents/%s", api.PublicWebURL, incident.ID)
	}

	if strings.HasPrefix(event.Name, "incident.responder.") {
		responderID := properties["id"]
		responder, err := pkgResponder.FindResponderByID(ctx, responderID)
		if err != nil {
			return nil, fmt.Errorf("error finding responder(id=%s): %v", responderID, err)
		} else if responder == nil {
			return nil, fmt.Errorf("responder(id=%s) not found", responderID)
		}

		incident, err := duty.FindCachedIncident(ctx, responder.IncidentID.String())
		if err != nil {
			return nil, fmt.Errorf("error finding incident(id=%s): %v", responder.IncidentID, err)
		} else if incident == nil {
			return nil, fmt.Errorf("incident(id=%s) not found", responder.IncidentID)
		}

		env["incident"] = incident.AsMap()
		env["responder"] = responder.AsMap()
		env["permalink"] = fmt.Sprintf("%s/incidents/%s", api.PublicWebURL, incident.ID)
	}

	if strings.HasPrefix(event.Name, "incident.comment.") {
		var comment models.Comment
		if err := ctx.DB().Where("id = ?", properties["id"]).Find(&comment).Error; err != nil {
			return nil, fmt.Errorf("error getting comment (id=%s)", properties["id"])
		}

		incident, err := duty.FindCachedIncident(ctx, comment.IncidentID.String())
		if err != nil {
			return nil, fmt.Errorf("error finding incident(id=%s): %v", comment.IncidentID, err)
		} else if incident == nil {
			return nil, fmt.Errorf("incident(id=%s) not found", comment.IncidentID)
		}

		author, err := duty.FindPerson(ctx, comment.CreatedBy.String())
		if err != nil {
			return nil, fmt.Errorf("error getting comment author (id=%s)", comment.CreatedBy)
		} else if author == nil {
			return nil, fmt.Errorf("comment author(id=%s) not found", comment.CreatedBy)
		}

		// TODO: extract out mentioned users' emails from the comment body

		env["incident"] = incident.AsMap()
		env["comment"] = comment.AsMap()
		env["author"] = author.AsMap()
		env["permalink"] = fmt.Sprintf("%s/incidents/%s", api.PublicWebURL, incident.ID)
	}

	if strings.HasPrefix(event.Name, "incident.dod.") {
		var evidence models.Evidence
		if err := ctx.DB().Where("id = ?", properties["id"]).Find(&evidence).Error; err != nil {
			return nil, err
		}

		var hypotheses models.Hypothesis
		if err := ctx.DB().Where("id = ?", evidence.HypothesisID).Find(&evidence).Find(&hypotheses).Error; err != nil {
			return nil, err
		}

		incident, err := duty.FindCachedIncident(ctx, hypotheses.IncidentID.String())
		if err != nil {
			return nil, fmt.Errorf("error finding incident(id=%s): %v", hypotheses.IncidentID, err)
		} else if incident == nil {
			return nil, fmt.Errorf("incident(id=%s) not found", hypotheses.IncidentID)
		}

		env["evidence"] = evidence.AsMap()
		env["hypotheses"] = hypotheses.AsMap()
		env["incident"] = incident.AsMap()
		env["permalink"] = fmt.Sprintf("%s/incidents/%s", api.PublicWebURL, incident.ID)
	}

	if strings.HasPrefix(event.Name, "component.status.") {
		componentID := properties["id"]

		component, err := duty.FindCachedComponent(ctx, componentID)
		if err != nil {
			return nil, fmt.Errorf("error finding component(id=%s): %v", componentID, err)
		} else if component == nil {
			return nil, fmt.Errorf("component(id=%s) not found", componentID)
		}

		agent, err := duty.FindCachedAgent(ctx, component.AgentID.String())
		if err != nil {
			return nil, fmt.Errorf("error finding agent: %v", err)
		} else if agent != nil {
			env["agent"] = agent.AsMap()
		}

		env["component"] = component.AsMap("checks", "incidents", "analysis", "components", "order", "relationship_id", "children", "parents")
		env["permalink"] = fmt.Sprintf("%s/topology/%s", api.PublicWebURL, componentID)
	}

	return env, nil
}
