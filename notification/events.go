package notification

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	sw "github.com/RussellLuo/slidingwindow"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/events"
	"github.com/flanksource/incident-commander/incidents/responder"
	"github.com/flanksource/incident-commander/logs"
	"github.com/flanksource/incident-commander/utils/expression"
	"github.com/flanksource/postq"
	"github.com/google/uuid"
	"github.com/samber/lo"
)

var (
	// rateLimiters per notification
	rateLimiters     = map[string]*sw.Limiter{}
	rateLimitersLock = sync.Mutex{}

	RateLimitWindow           = time.Hour * 4
	MaxNotificationsPerWindow = 50
)

func init() {
	events.Register(RegisterEvents)
}

func RegisterEvents(ctx context.Context) {
	events.RegisterSyncHandler(addNotificationEvent, append(api.EventStatusGroup, api.EventIncidentGroup...)...)
	events.RegisterAsyncHandler(sendNotifications, 1, 5, api.EventNotificationSend)
}

func getOrCreateRateLimiter(ctx context.Context, notificationID string) (*sw.Limiter, error) {
	rateLimitersLock.Lock()
	defer rateLimitersLock.Unlock()

	rl, ok := rateLimiters[notificationID]
	if ok {
		return rl, nil
	}

	window := ctx.Properties().Duration("notifications.max.window", RateLimitWindow)
	max := ctx.Properties().Int("notifications.max.count", MaxNotificationsPerWindow)

	// find the number of notifications sent for this notification in the last window period
	earliest, count, err := db.NotificationSendSummary(ctx, notificationID, window)
	if err != nil {
		return nil, err
	}

	rateLimiter, _ := sw.NewLimiter(window, int64(max), func() (sw.Window, sw.StopFunc) {
		win, stopper := NewLocalWindow()
		if count > 0 {
			// On init, sync the rate limiter with the notification send history.
			win.SetStart(earliest)
			win.AddCount(int64(count))
		}
		return win, stopper
	})

	rateLimiters[notificationID] = rateLimiter
	return rateLimiter, nil
}

// addNotificationEvent responds to a event that can possibly generate a notification.
// If a notification is found for the given event and passes all the filters, then
// a new `notification.send` event is created.
func addNotificationEvent(ctx context.Context, event postq.Event) error {
	notificationIDs, err := GetNotificationIDsForEvent(ctx, event.Name)
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
		n, err := GetNotification(ctx, id)
		if err != nil {
			return err
		}

		rateLimiter, err := getOrCreateRateLimiter(ctx, id)
		if err != nil {
			return fmt.Errorf("failed to create rate limiter: %w", err)
		}

		if !rateLimiter.Allow() {
			// rate limited notifications are simply dropped.
			logger.Warnf("notification(%s) rate limited for event=%s", id, event.Name)
			continue
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
			continue
		} else if !valid {
			continue
		}

		payloads, err := CreateNotificationSendPayloads(ctx, event, n, celEnv)
		if err != nil {
			return err
		}

		for _, payload := range payloads {
			newEvent := api.Event{
				Name:       api.EventNotificationSend,
				Properties: payload.AsMap(),
			}

			if err := ctx.DB().Clauses(events.EventQueueOnConflictClause).Create(&newEvent).Error; err != nil {
				return fmt.Errorf("failed to saved `notification.send` event for payload (%v): %w", payload.AsMap(), err)
			}
		}
	}

	return nil
}

// sendNotifications sends a notification for each of the given events - one at a time.
// It returns any events that failed to send.
func sendNotifications(ctx context.Context, events postq.Events) postq.Events {
	var failedEvents []postq.Event
	for _, e := range events {
		var payload NotificationEventPayload
		payload.FromMap(e.Properties)

		ctx.Debugf("[notification.send] %s  ", payload.EventName)

		notificationContext := NewContext(ctx, payload.NotificationID)
		notificationContext.WithSource(payload.EventName, payload.ID)

		logs.IfError(notificationContext.StartLog(), "error persisting start of notification send history")

		originalEvent := postq.Event{Name: payload.EventName, CreatedAt: payload.EventCreatedAt}
		if len(payload.Properties) > 0 {
			if err := json.Unmarshal(payload.Properties, &originalEvent.Properties); err != nil {
				e.SetError(err.Error())
				failedEvents = append(failedEvents, e)
				notificationContext.WithError(err.Error())
				continue
			}
		}

		celEnv, err := getEnvForEvent(ctx, originalEvent, e.Properties)
		if err != nil {
			e.SetError(err.Error())
			failedEvents = append(failedEvents, e)
			notificationContext.WithError(err.Error())
		} else if err := SendNotification(notificationContext, payload, celEnv); err != nil {
			e.SetError(err.Error())
			failedEvents = append(failedEvents, e)
			notificationContext.WithError(err.Error())
		}

		logs.IfError(notificationContext.EndLog(), "error persisting end of notification send history")
	}

	return failedEvents
}

// getEnvForEvent gets the environment variables for the given event
// that'll be passed to the cel expression or to the template renderer as a view.
func getEnvForEvent(ctx context.Context, event postq.Event, properties map[string]string) (map[string]any, error) {
	env := make(map[string]any)

	if strings.HasPrefix(event.Name, "check.") {
		checkID := properties["id"]
		lastRuntime := event.Properties["last_runtime"]

		check, err := query.FindCachedCheck(ctx, checkID)
		if err != nil {
			return nil, fmt.Errorf("error finding check: %v", err)
		} else if check == nil {
			return nil, fmt.Errorf("check(id=%s) not found", checkID)
		}

		canary, err := query.FindCachedCanary(ctx, check.CanaryID.String())
		if err != nil {
			return nil, fmt.Errorf("error finding canary: %v", err)
		} else if canary == nil {
			return nil, fmt.Errorf("canary(id=%s) not found", check.CanaryID)
		}

		agent, err := query.FindCachedAgent(ctx, check.AgentID.String())
		if err != nil {
			return nil, fmt.Errorf("error finding agent: %v", err)
		} else if agent != nil {
			env["agent"] = agent.AsMap()
		}

		summary, err := query.CheckSummary(ctx, query.CheckSummaryOptions{
			CheckID: lo.ToPtr((uuid.UUID)(check.ID)),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to get check summary: %w", err)
		} else if len(summary) > 0 {
			check.Uptime = summary[0].Uptime
			check.Latency = summary[0].Latency
		}

		var checkStatus models.CheckStatus
		if err := ctx.DB().Where("check_id = ?", checkID).Where("time = ?", lastRuntime).First(&checkStatus).Error; err != nil {
			return nil, fmt.Errorf("failed to get check status for check(%s/%s): %w", checkID, lastRuntime, err)
		}

		// The check that we supply on the template needs to have the status set
		// to the status corresponding to the time of event.
		check.Status = models.CheckHealthStatus(lo.Ternary(checkStatus.Status, models.CheckStatusHealthy, models.CheckStatusUnhealthy))

		env["status"] = checkStatus.AsMap()
		env["canary"] = canary.AsMap("spec")
		env["check"] = check.AsMap("spec")
		env["permalink"] = fmt.Sprintf("%s/health?layout=table&checkId=%s&timeRange=1h", api.PublicWebURL, check.ID)
	}

	if event.Name == "incident.created" || strings.HasPrefix(event.Name, "incident.status.") {
		incidentID := properties["id"]

		incident, err := query.GetCachedIncident(ctx, incidentID)
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
		responder, err := responder.FindResponderByID(ctx, responderID)
		if err != nil {
			return nil, fmt.Errorf("error finding responder(id=%s): %v", responderID, err)
		} else if responder == nil {
			return nil, fmt.Errorf("responder(id=%s) not found", responderID)
		}

		incident, err := query.GetCachedIncident(ctx, responder.IncidentID.String())
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

		incident, err := query.GetCachedIncident(ctx, comment.IncidentID.String())
		if err != nil {
			return nil, fmt.Errorf("error finding incident(id=%s): %v", comment.IncidentID, err)
		} else if incident == nil {
			return nil, fmt.Errorf("incident(id=%s) not found", comment.IncidentID)
		}

		author, err := query.FindPerson(ctx, comment.CreatedBy.String())
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

		incident, err := query.GetCachedIncident(ctx, hypotheses.IncidentID.String())
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

		component, err := query.GetCachedComponent(ctx, componentID)
		if err != nil {
			return nil, fmt.Errorf("error finding component(id=%s): %v", componentID, err)
		} else if component == nil {
			return nil, fmt.Errorf("component(id=%s) not found", componentID)
		}

		agent, err := query.FindCachedAgent(ctx, component.AgentID.String())
		if err != nil {
			return nil, fmt.Errorf("error finding agent: %v", err)
		} else if agent != nil {
			env["agent"] = agent.AsMap()
		}

		statusInEvent := strings.TrimPrefix(event.Name, "component.status.")
		component.Status = types.ComponentStatus(statusInEvent)

		env["component"] = component.AsMap("checks", "incidents", "analysis", "components", "order", "relationship_id", "children", "parents")
		env["permalink"] = fmt.Sprintf("%s/topology/%s", api.PublicWebURL, componentID)
	}

	if strings.HasPrefix(event.Name, "config.") {
		configID := properties["id"]

		config, err := query.GetCachedConfig(ctx, configID)
		if err != nil {
			return nil, fmt.Errorf("error finding config(id=%s): %v", configID, err)
		} else if config == nil {
			return nil, fmt.Errorf("config(id=%s) not found", configID)
		}

		agent, err := query.FindCachedAgent(ctx, config.AgentID.String())
		if err != nil {
			return nil, fmt.Errorf("error finding agent: %v", err)
		} else if agent != nil {
			env["agent"] = agent.AsMap()
		}

		statusInEvent := strings.TrimPrefix(event.Name, "config.")
		config.Status = &statusInEvent

		env["config"] = config.AsMap("last_scraped_time", "path", "parent_id")
		env["permalink"] = fmt.Sprintf("%s/catalog/%s", api.PublicWebURL, configID)
	}

	return env, nil
}
