package notification

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	sw "github.com/RussellLuo/slidingwindow"
	"github.com/flanksource/commons/text"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/gomplate/v3"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/events"
	"github.com/flanksource/incident-commander/incidents/responder"
	"github.com/flanksource/incident-commander/logs"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"gorm.io/gorm/clause"
)

var (
	// rateLimiters per notification
	rateLimiters     = map[string]*sw.Limiter{}
	rateLimitersLock = sync.Mutex{}

	RateLimitWindow           = time.Hour * 4
	MaxNotificationsPerWindow = 50

	EventRing *events.EventRing
)

func init() {
	events.Register(RegisterEvents)
}

func RegisterEvents(ctx context.Context) {
	EventRing = events.NewEventRing(ctx.Properties().Int("events.audit.size", events.DefaultEventLogSize))
	nh := notificationHandler{Ring: EventRing}
	events.RegisterSyncHandler(nh.addNotificationEvent, append(api.EventStatusGroup, api.EventIncidentGroup...)...)

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

type notificationHandler struct {
	Ring *events.EventRing
}

// Check if notification can be sent in the interval based on group by, returns true if it can be sent
func checkRepeatInterval(ctx context.Context, n NotificationWithSpec, event models.Event) (bool, error) {
	if n.RepeatInterval == "" {
		return true, nil
	}

	interval, err := text.ParseDuration(n.RepeatInterval)
	if err != nil {
		return false, fmt.Errorf("error parsing repeat interval[%s] to time.Duration: %w", n.RepeatInterval, err)
	}

	clauses := []clause.Expression{
		clause.Eq{Column: "notification_id", Value: n.ID.String()},
		clause.Eq{Column: "resource_id", Value: event.Properties["id"]},
		clause.Eq{Column: "source_event", Value: event.Name},
	}

	var exists bool
	tx := ctx.DB().Model(&models.NotificationSendHistory{}).Clauses(clauses...).
		Select(fmt.Sprintf("(NOW() - created_at) <= '%d minutes'::INTERVAL", int(interval.Minutes()))).
		Order("created_at DESC").Limit(1).Scan(&exists)
	if tx.Error != nil {
		return false, fmt.Errorf("error querying db for last send notification[%s]: %w", n.ID, err)
	}

	return !exists, nil
}

// addNotificationEvent responds to a event that can possibly generate a notification.
// If a notification is found for the given event and passes all the filters, then
// a new `notification.send` event is created.
func (t *notificationHandler) addNotificationEvent(ctx context.Context, event models.Event) error {
	notificationIDs, err := GetNotificationIDsForEvent(ctx, event.Name)
	if err != nil {
		return err
	}

	if len(notificationIDs) == 0 {
		return nil
	}

	celEnv, err := GetEnvForEvent(ctx, event)
	if err != nil {
		return err
	}

	t.Ring.Add(event, celEnv.AsMap())

	silencedResource := getSilencedResourceFromCelEnv(celEnv)
	matchingSilences, err := db.GetMatchingNotificationSilences(ctx, silencedResource)
	if err != nil {
		return err
	}

	for _, id := range notificationIDs {
		if err := addNotificationEvent(ctx, id, celEnv.AsMap(), event, matchingSilences); err != nil {
			return fmt.Errorf("failed to add notification.send event for event=%s notification=%s: %w", event.Name, id, err)
		}
	}

	return nil
}

func addNotificationEvent(ctx context.Context, id string, celEnv map[string]any, event models.Event, matchingSilences []models.NotificationSilence) error {
	n, err := GetNotification(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to get notification %s: %w", id, err)
	}

	if !n.HasRecipients() {
		return nil
	}

	// This gets unset by the database trigger: reset_notification_error_before_update_trigger
	if n.Error != nil {
		// A notification that currently has errors is skipped.
		return nil
	}

	if n.Filter != "" {
		if valid, err := gomplate.RunTemplateBool(celEnv, gomplate.Template{Expression: n.Filter}); err != nil {
			// On invalid spec error, we store the error on the notification itself and exit out.
			logs.IfError(db.UpdateNotificationError(ctx, id, err.Error()), "failed to update notification")
			return nil
		} else if !valid {
			return nil
		}
	}

	payloads, err := CreateNotificationSendPayloads(ctx, event, n, celEnv)
	if err != nil {
		return fmt.Errorf("failed to create notification.send payloads: %w", err)
	}

	rateLimiter, err := getOrCreateRateLimiter(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to create rate limiter: %w", err)
	}

	passesRepeatInterval, err := checkRepeatInterval(ctx, *n, event)
	if err != nil {
		// If there are any errors in calculating interval, we send the notification and log the error
		ctx.Errorf("error checking repeat interval for notification[%s]: %v", n.ID, err)
		passesRepeatInterval = true
	}

	for _, payload := range payloads {
		if shouldSilence(ctx, celEnv, matchingSilences) {
			ctx.Logger.V(6).Infof("silencing notification for event %s due to %d matching silences", event.ID, matchingSilences)
			ctx.Counter("notification_silenced", "id", id, "resource", payload.ID.String()).Add(1)

			history := models.NotificationSendHistory{
				NotificationID: n.ID,
				ResourceID:     payload.ID,
				SourceEvent:    event.Name,
				Status:         models.NotificationStatusSilenced,
			}
			if err := db.SaveUnsentNotificationToHistory(ctx, history); err != nil {
				return fmt.Errorf("failed to save silenced notification history: %w", err)
			}

			continue
		}

		// Repeat interval check
		if n.RepeatInterval != "" && !passesRepeatInterval {
			ctx.Logger.V(6).Infof("skipping notification[%s] due to repeat interval", n.ID)
			ctx.Counter("notification_skipped_by_repeat_interval", "id", id, "resource", payload.ID.String(), "source_event", event.Name).Add(1)

			history := models.NotificationSendHistory{
				NotificationID: n.ID,
				ResourceID:     payload.ID,
				SourceEvent:    event.Name,
				Status:         models.NotificationStatusRepeatInterval,
			}
			if err := db.SaveUnsentNotificationToHistory(ctx, history); err != nil {
				return fmt.Errorf("failed to save silenced notification history: %w", err)
			}

			continue
		}

		if !rateLimiter.Allow() {
			// rate limited notifications are simply dropped.
			ctx.Warnf("notification rate limited event=%s id=%s ", event.Name, id)
			ctx.Counter("notification_rate_limited", "id", id).Add(1)
			continue
		}

		newEvent := models.Event{
			Name:       api.EventNotificationSend,
			Properties: payload.AsMap(),
		}
		if err := ctx.DB().Clauses(events.EventQueueOnConflictClause).Create(&newEvent).Error; err != nil {
			return fmt.Errorf("failed to saved `notification.send` event for payload (%v): %w", payload.AsMap(), err)
		}
	}

	return nil
}

// sendNotifications sends a notification for each of the given events - one at a time.
// It returns any events that failed to send.
func sendNotifications(ctx context.Context, events models.Events) models.Events {
	ctx = ctx.WithName("notifications")

	var failedEvents []models.Event
	for _, e := range events {
		var payload NotificationEventPayload
		payload.FromMap(e.Properties)

		if err := sendNotification(ctx, payload); err != nil {
			e.SetError(err.Error())
			failedEvents = append(failedEvents, e)
			continue
		}
	}

	return failedEvents
}

func sendPendingNotification(ctx context.Context, history models.NotificationSendHistory, payload NotificationEventPayload) error {
	notificationContext := NewContext(ctx, payload.NotificationID).WithHistory(history)

	ctx.Debugf("[notification.send] %s  ", payload.EventName)
	notificationContext.WithSource(payload.EventName, payload.ID)

	err := _sendNotification(notificationContext, true, payload)
	if err != nil {
		notificationContext.WithError(err)
	}

	logs.IfError(notificationContext.EndLog(), "error persisting end of notification send history")
	return err
}

func sendNotification(ctx context.Context, payload NotificationEventPayload) error {
	notificationContext := NewContext(ctx, payload.NotificationID)

	ctx.Debugf("[notification.send] %s  ", payload.EventName)
	notificationContext.WithSource(payload.EventName, payload.ID)
	logs.IfError(notificationContext.StartLog(), "error persisting start of notification send history")

	err := _sendNotification(notificationContext, false, payload)
	if err != nil {
		notificationContext.WithError(err)
	}

	logs.IfError(notificationContext.EndLog(), "error persisting end of notification send history")
	return err
}

func _sendNotification(ctx *Context, noWait bool, payload NotificationEventPayload) error {
	originalEvent := models.Event{Name: payload.EventName, CreatedAt: payload.EventCreatedAt}
	if len(payload.Properties) > 0 {
		if err := json.Unmarshal(payload.Properties, &originalEvent.Properties); err != nil {
			return fmt.Errorf("failed to unmarshal properties: %w", err)
		}
	}

	celEnv, err := GetEnvForEvent(ctx.Context, originalEvent)
	if err != nil {
		return fmt.Errorf("failed to get cel env: %w", err)
	}

	nn, err := GetNotification(ctx.Context, payload.NotificationID.String())
	if err != nil {
		return fmt.Errorf("failed to get notification: %w", err)
	}

	if !noWait && nn.WaitFor != nil {
		// delayed notifications are saved to history with a pending status
		// and are later consumed by a job.
		ctx.log.NotBefore = lo.ToPtr(ctx.log.CreatedAt.Add(*nn.WaitFor))
		ctx.log.Status = models.NotificationStatusPending
		ctx.log.Payload = payload.AsMap()
	} else {
		if err := PrepareAndSendEventNotification(ctx, payload, celEnv.AsMap()); err != nil {
			return fmt.Errorf("failed to send notification for event: %w", err)
		}

		ctx.log.Sent()
	}

	return nil
}

func isHealthReportable(events []string, previousHealth, currentHealth models.Health) bool {
	isCurrentHealthInNotification := lo.ContainsBy(events, func(event string) bool {
		return api.EventToHealth(event) == currentHealth
	})

	if !isCurrentHealthInNotification {
		// Either the notification has changed
		// or the health of the resource has changed to something that the notification isn't configured for
		return false
	}

	return previousHealth == currentHealth
}

// GetEnvForEvent gets the environment variables for the given event
// that'll be passed to the cel expression or to the template renderer as a view.
func GetEnvForEvent(ctx context.Context, event models.Event) (*celVariables, error) {
	var env celVariables

	if strings.HasPrefix(event.Name, "check.") {
		checkID := event.Properties["id"]
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
			env.Agent = agent
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

		env.CheckStatus = &checkStatus
		env.Canary = canary
		env.Check = check
		env.Permalink = fmt.Sprintf("%s/health?layout=table&checkId=%s&timeRange=1h", api.FrontendURL, check.ID)
	}

	if event.Name == "incident.created" || strings.HasPrefix(event.Name, "incident.status.") {
		incidentID := event.Properties["id"]

		incident, err := query.GetCachedIncident(ctx, incidentID)
		if err != nil {
			return nil, fmt.Errorf("error finding incident(id=%s): %v", incidentID, err)
		} else if incident == nil {
			return nil, fmt.Errorf("incident(id=%s) not found", incidentID)
		}

		env.Incident = incident
		env.Permalink = fmt.Sprintf("%s/incidents/%s", api.FrontendURL, incident.ID)
	}

	if strings.HasPrefix(event.Name, "incident.responder.") {
		responderID := event.Properties["id"]
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

		env.Incident = incident
		env.Responder = responder
		env.Permalink = fmt.Sprintf("%s/incidents/%s", api.FrontendURL, incident.ID)
	}

	if strings.HasPrefix(event.Name, "incident.comment.") {
		var comment models.Comment
		if err := ctx.DB().Where("id = ?", event.Properties["id"]).Find(&comment).Error; err != nil {
			return nil, fmt.Errorf("error getting comment (id=%s)", event.Properties["id"])
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

		env.Incident = incident
		env.Comment = &comment
		env.Author = author
		env.Permalink = fmt.Sprintf("%s/incidents/%s", api.FrontendURL, incident.ID)
	}

	if strings.HasPrefix(event.Name, "incident.dod.") {
		var evidence models.Evidence
		if err := ctx.DB().Where("id = ?", event.Properties["id"]).Find(&evidence).Error; err != nil {
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

		env.Evidence = &evidence
		env.Hypothesis = &hypotheses
		env.Incident = incident
		env.Permalink = fmt.Sprintf("%s/incidents/%s", api.FrontendURL, incident.ID)
	}

	if strings.HasPrefix(event.Name, "component.") {
		componentID := event.Properties["id"]

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
			env.Agent = agent
		}

		// Use the health, description & status from the time of event
		component.Health = lo.ToPtr(models.Health(strings.TrimPrefix(event.Name, "component.")))
		component.Description = event.Properties["description"]
		component.Status = types.ComponentStatus(event.Properties["status"])

		env.Component = component
		env.Permalink = fmt.Sprintf("%s/topology/%s", api.FrontendURL, componentID)
	}

	if strings.HasPrefix(event.Name, "config.") {
		configID := event.Properties["id"]

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
			env.Agent = agent
		}

		eventSuffix := strings.TrimPrefix(event.Name, "config.")
		isStateUpdateEvent := slices.Contains([]string{
			api.EventConfigChanged,
			api.EventConfigCreated,
			api.EventConfigDeleted,
			api.EventConfigUpdated,
		}, event.Name)
		if isStateUpdateEvent {
			env.NewState = eventSuffix
		} else {
			// Use the health, description & status from the time of event
			config.Health = lo.ToPtr(models.Health(eventSuffix))
			config.Description = lo.ToPtr(event.Properties["description"])
			config.Status = lo.ToPtr(event.Properties["status"])
		}

		env.ConfigItem = config
		env.Permalink = fmt.Sprintf("%s/catalog/%s", api.FrontendURL, configID)
	}

	env.SetSilenceURL(api.FrontendURL)
	return &env, nil
}

func shouldSilence(ctx context.Context, celEnv map[string]any, matchingSilences []models.NotificationSilence) bool {
	for _, silence := range matchingSilences {
		if silence.Filter != "" {
			res, err := ctx.RunTemplate(gomplate.Template{Expression: string(silence.Filter)}, celEnv)
			if err != nil {
				ctx.Errorf("failed to run silence filter expression(%s): %v", silence.Filter, err)
				logs.IfError(db.UpdateNotificationSilenceError(ctx, silence.ID.String(), err.Error()), "failed to update notification silence")
				continue
			} else if ok, err := strconv.ParseBool(res); err != nil {
				ctx.Errorf("silence filter did not return a boolean value(%s): %v", silence.Filter, err)
				logs.IfError(db.UpdateNotificationSilenceError(ctx, silence.ID.String(), err.Error()), "failed to update notification silence")
			} else if !ok {
				continue
			}
		}

		return true
	}

	return false
}
