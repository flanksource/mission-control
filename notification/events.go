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
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/pkg/tokenizer"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/gomplate/v3"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"gorm.io/gorm/clause"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/events"
	"github.com/flanksource/incident-commander/incidents/responder"
	"github.com/flanksource/incident-commander/logs"
)

const DefaultGroupByInterval = time.Hour * 24

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
func checkRepeatInterval(ctx context.Context, n NotificationWithSpec, groupID *uuid.UUID, resourceID, sourceEvent string) (*models.NotificationSendHistory, error) {
	if n.RepeatInterval == nil {
		return nil, nil
	}

	clauses := []clause.Expression{
		clause.Eq{Column: "notification_id", Value: n.ID.String()},
		clause.Eq{Column: "status", Value: models.NotificationStatusSent},
		clause.Eq{Column: "source_event", Value: sourceEvent},
	}

	if groupID != nil {
		clauses = append(clauses, clause.Or(
			clause.Eq{Column: "resource_id", Value: resourceID},
			clause.Eq{Column: "group_id", Value: groupID},
		))
	} else {
		clauses = append(clauses, clause.Eq{Column: "resource_id", Value: resourceID})
	}

	var sendHistory models.NotificationSendHistory
	tx := ctx.DB().Clauses(clauses...).
		Select("id", "group_id").
		Where(fmt.Sprintf("(NOW() - created_at) <= '%d minutes'::INTERVAL", int(n.RepeatInterval.Minutes()))).
		Order("created_at DESC").Limit(1).Find(&sendHistory)
	if tx.Error != nil {
		return nil, fmt.Errorf("error querying db for last send notification[%s]: %w", n.ID, tx.Error)
	}

	if sendHistory.ID == uuid.Nil {
		return nil, nil
	}

	return &sendHistory, nil
}

// addNotificationEvent responds to a event that can possibly generate a notification.
// If a notification is found for the given event and passes all the filters, then
// a new `notification.send` event is created.
func (t *notificationHandler) addNotificationEvent(ctx context.Context, event models.Event) error {
	// We need an authorized RBAC subject to read connections.
	// So we use the system user as the subject.
	ctx = ctx.WithSubject(api.SystemUserID.String())

	celEnv, err := GetEnvForEvent(ctx, event)
	if err != nil {
		return ctx.Oops().Wrapf(err, "failed to get env for event %s %s", event.ID, event.Name)
	}

	if lo.Contains(api.ConfigEvents, event.Name) {
		if err := resolveGroupMembership(ctx, celEnv, event.EventID.String()); err != nil {
			return ctx.Oops().Wrapf(err, "failed to resolve group membership for event")
		}
	}

	notificationIDs, err := GetNotificationIDsForEvent(ctx, event.Name)
	if err != nil {
		return ctx.Oops().Wrapf(err, "failed to get notification ids for event")
	}

	if len(notificationIDs) == 0 {
		return nil
	}

	t.Ring.Add(event, celEnv.AsMap(ctx))

	silencedResource := getSilencedResourceFromCelEnv(celEnv)
	matchingSilences, err := db.GetMatchingNotificationSilences(ctx, silencedResource)
	if err != nil {
		return ctx.Oops().Wrapf(err, "failed to get matching notification silences")
	}

	for _, id := range notificationIDs {
		if err := addNotificationEvent(ctx, id, celEnv, event, matchingSilences); err != nil {
			return ctx.Oops().Wrapf(err, "failed to add notification.send event for event=%s notification=%s", event.Name, id)
		}
	}

	return nil
}

// resolveGroupMembership removes any resources from notification group that
// no longer match the notification event & filter.
func resolveGroupMembership(ctx context.Context, celEnv *celVariables, configID string) error {
	var notificationGroups []models.NotificationGroup
	sql := `SELECT id, notification_id FROM notification_groups WHERE id IN (SELECT group_id FROM notification_group_resources WHERE config_id = ?)`
	if err := ctx.DB().Raw(sql, configID).Scan(&notificationGroups).Error; err != nil {
		return ctx.Oops().Wrapf(err, "failed to get notifications for config %s", configID)
	}

	for _, ng := range notificationGroups {
		if resolved, err := resolveGroupMembershipForNotification(ctx, celEnv, configID, ng.NotificationID.String()); err != nil {
			return ctx.Oops().Wrapf(err, "failed to resolve notification group %s", ng.ID)
		} else if resolved {
			if err := ctx.DB().Model(&models.NotificationGroupResource{}).
				Where("group_id = ? AND config_id = ?", ng.ID, configID).
				UpdateColumn("resolved_at", duty.Now()).Error; err != nil {
				return ctx.Oops().Wrapf(err, "failed to delete config from notification group %s", ng.ID)
			}
		}
	}

	return nil
}

func resolveGroupMembershipForNotification(ctx context.Context, celEnv *celVariables, configID, notificationID string) (bool, error) {
	notification, err := GetNotification(ctx, notificationID)
	if err != nil {
		return false, ctx.Oops().Wrapf(err, "failed to get notification %s", notificationID)
	}

	var config models.ConfigItem
	if err := ctx.DB().Where("id = ?", configID).Find(&config).Error; err != nil {
		return false, ctx.Oops().Wrapf(err, "failed to get config %s", configID)
	} else if config.ID == uuid.Nil {
		return false, ctx.Oops().Wrapf(err, "config not found %s", configID)
	}

	if !lo.Contains(notification.Events, fmt.Sprintf("config.%s", string(*config.Health))) {
		return true, nil
	}

	if notification.Filter != "" {
		valid, err := ctx.RunTemplateBool(gomplate.Template{Expression: notification.Filter}, celEnv.AsMap(ctx))
		if err != nil {
			return false, ctx.Oops().Wrapf(err, "failed to validate notification filter for notification %s", notificationID)
		}

		if !valid {
			return true, nil
		}
	}

	return false, nil
}

func addNotificationEvent(ctx context.Context, id string, celEnv *celVariables, event models.Event, matchingSilences []models.NotificationSilence) error {
	n, err := GetNotification(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to get notification %s: %w", id, err)
	}

	if !n.HasRecipients() {
		return nil
	}

	// This gets unset by the database trigger: reset_notification_error_before_update_trigger
	if n.Error != nil {
		// We ignore error and retry after an hour to see if it works
		if n.ErrorAt != nil && time.Since(*n.ErrorAt) >= ctx.Properties().Duration("notifications.error_reset_duration", 1*time.Hour) {
			if err := db.ResetNotificationError(ctx, id); err != nil {
				return fmt.Errorf("error resetting notification[%s] error: %w", id, err)
			}
			// Remove this notification from cache
			PurgeCache(id)
		} else {
			return nil
		}
	}

	if n.Filter != "" {
		if valid, err := ctx.RunTemplateBool(gomplate.Template{Expression: n.Filter}, celEnv.AsMap(ctx)); err != nil {
			// On invalid spec error, we store the error on the notification itself and exit out.
			logs.IfError(db.SetNotificationError(ctx, id, err.Error()), "failed to update notification")
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

	for _, payload := range payloads {
		if blocker, err := processNotificationConstraints(ctx, *n, payload, celEnv, matchingSilences); err != nil {
			return fmt.Errorf("failed to check all conditions for notification[%s]: %w", n.ID, err)
		} else if blocker != nil {
			history := models.NotificationSendHistory{
				NotificationID:            n.ID,
				ResourceID:                payload.ResourceID,
				ResourceHealth:            payload.ResourceHealth,
				ResourceStatus:            payload.ResourceStatus,
				ResourceHealthDescription: payload.ResourceHealthDescription,
				SourceEvent:               payload.EventName,
				Status:                    blocker.BlockedWithStatus,
				ParentID:                  blocker.ParentID,
				SilencedBy:                blocker.SilencedBy,
				GroupID:                   payload.GroupID,
				PersonID:                  payload.PersonID,
				TeamID:                    payload.TeamID,
				ConnectionID:              payload.Connection,
				Body:                      payload.Body,
			}

			if err := db.SaveUnsentNotificationToHistory(ctx, history); err != nil {
				return fmt.Errorf("failed to save %s notification history: %w", blocker.BlockedWithStatus, err)
			}

			continue
		}

		if !rateLimiter.Allow() {
			// rate limited notifications are simply dropped.
			ctx.Warnf("notification rate limited event=%s notification=%s resource=%s (health=%s, status=%s, description=%s)",
				event.Name, id, payload.ResourceID, payload.ResourceHealth, payload.ResourceStatus, payload.ResourceHealthDescription)
			ctx.Counter("notification_rate_limited", "id", id).Add(1)
			continue
		}

		// Notifications that have waitFor configured go through a waiting stage
		// while the rest are sent immediately.
		if n.WaitFor != nil {
			pendingHistory := models.NotificationSendHistory{
				NotificationID:            n.ID,
				ResourceID:                payload.ResourceID,
				ResourceHealth:            payload.ResourceHealth,
				ResourceStatus:            payload.ResourceStatus,
				ResourceHealthDescription: payload.ResourceHealthDescription,
				SourceEvent:               event.Name,
				Payload:                   payload.AsMap(),
				GroupID:                   payload.GroupID,
				Status:                    models.NotificationStatusPending,
				NotBefore:                 lo.ToPtr(time.Now().Add(*n.WaitFor)),
				PersonID:                  payload.PersonID,
				ConnectionID:              payload.Connection,
				TeamID:                    payload.TeamID,
				Body:                      payload.Body,
			}

			if err := ctx.DB().Create(&pendingHistory).Error; err != nil {
				return fmt.Errorf("failed to save pending notification: %w", err)
			}
		} else {
			newEvent := models.Event{
				Name:       api.EventNotificationSend,
				Properties: payload.WithIDSet().AsMap(),
			}
			if err := ctx.DB().Clauses(events.EventQueueOnConflictClause).Create(&newEvent).Error; err != nil {
				return fmt.Errorf("failed to saved `notification.send` event for payload (%v): %w", payload.AsMap(), err)
			}
		}
	}

	return nil
}

type validateResult struct {
	BlockedWithStatus string
	ParentID          *uuid.UUID
	SilencedBy        *uuid.UUID
}

// processNotificationConstraints checks if the notification passes multiple constraints
// like repeat interval, inhibition, etc.
func processNotificationConstraints(ctx context.Context,
	n NotificationWithSpec,
	payload NotificationEventPayload,
	celEnv *celVariables,
	matchingSilences []models.NotificationSilence,
) (*validateResult, error) {
	sourceEvent := payload.EventName

	// Repeat interval check
	if n.RepeatInterval != nil {
		blockingSendHistory, err := checkRepeatInterval(ctx, n, payload.GroupID, payload.ResourceID.String(), sourceEvent)
		if err != nil {
			// If there are any errors in calculating interval, we send the notification and log the error
			ctx.Errorf("error checking repeat interval for notification[%s]: %v", n.ID, err)
		}

		if blockingSendHistory != nil {
			ctx.Logger.V(6).Infof("skipping notification[%s] due to repeat interval", n.ID)
			ctx.Counter("notification_skipped_by_repeat_interval", "id", n.ID.String(), "resource", payload.ResourceID.String(), "source_event", sourceEvent).Add(1)

			result := &validateResult{
				BlockedWithStatus: models.NotificationStatusRepeatInterval,
				ParentID:          &blockingSendHistory.ID,
			}

			return result, nil
		}
	}

	if silencedBy := getFirstSilencer(ctx, celEnv, matchingSilences); silencedBy != nil {
		ctx.Logger.V(6).Infof("silencing notification for event %s due to %d matching silences", sourceEvent, matchingSilences)
		ctx.Counter("notification_silenced", "id", n.ID.String(), "resource", payload.ResourceID.String()).Add(1)
		return &validateResult{
			BlockedWithStatus: models.NotificationStatusSilenced,
			SilencedBy:        &silencedBy.ID,
		}, nil
	}

	if len(n.Inhibitions) > 0 && n.RepeatInterval != nil && celEnv.ConfigItem != nil {
		inhibitor, err := checkInhibition(ctx, n, celEnv.SelectableResource())
		if err != nil {
			return nil, fmt.Errorf("failed to check inhibition for notification[%s]: %w", n.ID, err)
		}

		if inhibitor != nil {
			ctx.Logger.V(6).Infof("skipping notification[%s] due to inhibition", n.ID)
			ctx.Counter("notification_inhibited", "id", n.ID.String(), "resource", payload.ResourceID.String(), "source_event", sourceEvent).Add(1)

			return &validateResult{
				BlockedWithStatus: models.NotificationStatusInhibited,
				ParentID:          inhibitor,
			}, nil
		}
	}

	return nil, nil
}

func checkInhibition(ctx context.Context, notif NotificationWithSpec, resource types.ResourceSelectable) (*uuid.UUID, error) {
	// Note: we use the repeat interval as the inhibition window.
	inhibitionWindow := notif.RepeatInterval
	if inhibitionWindow == nil {
		return nil, nil
	}

	resourceID, err := uuid.Parse(resource.GetID())
	if err != nil {
		return nil, fmt.Errorf("failed to parse resource id: %w", err)
	}

	for _, inhibition := range notif.Inhibitions {
		if !lo.Contains(inhibition.To, resource.GetType()) {
			continue
		}

		rq := query.RelationQuery{
			ID:       resourceID,
			MaxDepth: inhibition.Depth,
		}

		// We need to invert the direction because we're looking from the "to" perspective.
		switch inhibition.Direction {
		case query.Outgoing:
			rq.Relation = query.Incoming
		case query.Incoming:
			rq.Relation = query.Outgoing
		default:
			rq.Relation = query.All
		}
		if inhibition.Soft {
			rq.Incoming = query.Both
			rq.Outgoing = query.Both
		} else {
			rq.Incoming = query.Hard
			rq.Outgoing = query.Hard
		}

		relatedConfigs, err := query.GetRelatedConfigs(ctx, rq)
		if err != nil {
			return nil, fmt.Errorf("failed to get related configs: %w", err)
		}

		var relatedIDs []uuid.UUID
		for _, rc := range relatedConfigs {
			if rc.Type == inhibition.From && rc.ID.String() != resource.GetID() {
				relatedIDs = append(relatedIDs, rc.ID)
			}
		}

		var id string
		if err := ctx.DB().Model(&models.NotificationSendHistory{}).
			Select("id").
			Where("notification_id = ?", notif.ID).
			Where("status = ?", models.NotificationStatusSent).
			Where("resource_id IN ?", relatedIDs).
			Where(fmt.Sprintf("created_at >= NOW() - INTERVAL '%f MINUTES'", inhibitionWindow.Minutes())).
			Limit(1).
			Find(&id).Error; err != nil {
			return nil, fmt.Errorf("failed to get related notification count: %w", err)
		}

		if id != "" {
			return lo.ToPtr(uuid.MustParse(id)), nil
		}
	}

	return nil, nil
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

func sendFallbackNotification(ctx context.Context, sendHistory models.NotificationSendHistory) error {
	notif, err := GetNotification(ctx, sendHistory.NotificationID.String())
	if err != nil {
		return fmt.Errorf("failed to get notification[%s]: %w", sendHistory.NotificationID, err)
	}

	var payload NotificationEventPayload
	payload.FromMap(sendHistory.Payload)

	payload.PersonID = notif.FallbackPersonID
	payload.TeamID = notif.FallbackTeamID
	payload.PlaybookID = notif.FallbackPlaybookID
	payload.CustomService = notif.FallbackCustomNotification

	if err := sendPendingNotification(ctx, sendHistory, payload); err != nil {
		return fmt.Errorf("failed to send notification: %w", err)
	} else if dberr := ctx.DB().Model(&models.NotificationSendHistory{}).Where("id = ?", sendHistory.ID).UpdateColumns(map[string]any{
		"status": models.NotificationStatusSent,
	}).Error; dberr != nil {
		return fmt.Errorf("failed to save notification status as sent: %w", dberr)
	}

	return nil
}

func sendPendingNotification(ctx context.Context, history models.NotificationSendHistory, payload NotificationEventPayload) error {
	notificationContext := NewContext(ctx.WithSubject(payload.NotificationID.String()), payload.NotificationID).WithHistory(history)
	ctx.Debugf("[notification.send] %s ", payload.EventName)
	notificationContext.WithSource(payload.EventName, payload.ResourceID)
	notificationContext.WithGroupID(payload.GroupID)

	err := _sendNotification(notificationContext, payload)
	if err != nil {
		notificationContext.WithError(err)
	}

	logs.IfError(notificationContext.EndLog(), "error persisting end of notification send history")
	return err
}

func calculateGroupByHash(ctx context.Context, groupBy []string, resourceID, event string) (string, error) {
	var hash string
	if strings.HasPrefix(event, "config") {
		ci, err := query.GetCachedConfig(ctx, resourceID)
		if err != nil {
			return "", fmt.Errorf("error fetching cached config for group by hash: %w", err)
		}
		for _, group := range groupBy {
			switch {
			case strings.HasPrefix(group, "tag:"):
				hash += ci.Tags[strings.ReplaceAll(group, "tag:", "")]
			case strings.HasPrefix(group, "label:"):
				hash += lo.FromPtr(ci.Labels)[strings.ReplaceAll(group, "label:", "")]
			case group == "type":
				hash += lo.FromPtr(ci.Type)
			case group == "description" || group == "status_reason":
				description := strings.ReplaceAll(lo.FromPtr(ci.Description), lo.FromPtr(ci.Name), "<name>")
				hash += tokenizer.TokenizedHash(description)
			}
		}
	}
	if strings.HasPrefix(event, "component") {
		comp, err := query.GetCachedComponent(ctx, resourceID)
		if err != nil {
			return "", fmt.Errorf("error fetching cached component for group by hash: %w", err)
		}
		for _, group := range groupBy {
			switch {
			case strings.HasPrefix(group, "label:"):
				hash += comp.Labels[strings.ReplaceAll(group, "label:", "")]
			case group == "type":
				hash += comp.Type
			case group == "description" || group == "status_reason":
				description := strings.ReplaceAll(comp.StatusReason, comp.Name, "<name>")
				hash += tokenizer.TokenizedHash(description)
			}
		}
	}
	if strings.HasPrefix(event, "check") {
		check, err := query.FindCachedCheck(ctx, resourceID)
		if err != nil {
			return "", fmt.Errorf("error fetching cached check for group by hash: %w", err)
		}
		if check == nil {
			return "", fmt.Errorf("check[%s] not found", resourceID)
		}
		for _, group := range groupBy {
			switch {
			case strings.HasPrefix(group, "label:"):
				hash += check.Labels[strings.ReplaceAll(group, "label:", "")]
			case group == "type":
				hash += check.Type
			case group == "description" || group == "status_reason":
				description := strings.ReplaceAll(check.Description, check.Name, "<name>")
				hash += tokenizer.TokenizedHash(description)
			}
		}
	}

	return hash, nil
}

func sendNotification(ctx context.Context, payload NotificationEventPayload) error {
	notificationContext := NewContext(ctx.WithSubject(payload.NotificationID.String()), payload.NotificationID)
	ctx.Debugf("[notification.send] %s  ", payload.EventName)
	notificationContext.WithSource(payload.EventName, payload.ResourceID)
	notificationContext.WithGroupID(payload.GroupID)

	logs.IfError(notificationContext.StartLog(), "error persisting start of notification send history")

	err := _sendNotification(notificationContext, payload)
	if err != nil {
		notificationContext.WithError(err)
	}

	logs.IfError(notificationContext.EndLog(), "error persisting end of notification send history")
	return err
}

func _sendNotification(ctx *Context, payload NotificationEventPayload) error {
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

	if payload.GroupID != nil {
		celEnv.GroupedResources, err = db.GetGroupedResources(ctx.Context, *payload.GroupID, payload.ResourceID.String())
		if err != nil {
			return ctx.Oops().Wrapf(err, "failed to get grouped resources for notification[%s]", payload.NotificationID)
		}
	}

	nn, err := GetNotification(ctx.Context, payload.NotificationID.String())
	if err != nil {
		return fmt.Errorf("failed to get notification: %w", err)
	}

	ctx.log.Payload = payload.AsMap()

	if payload.PlaybookID != nil {
		if err := triggerPlaybookRun(ctx, celEnv, *payload.PlaybookID); err != nil {
			return err
		}

		ctx.log.PendingPlaybookRun()
	} else {
		traceLog("NotificationID=%s Resource=[%s/%s] Sending ...", nn.ID, payload.EventName, payload.ResourceID)
		if err := PrepareAndSendEventNotification(ctx, payload, celEnv); err != nil {
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
		checkID := event.EventID.String()
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
		incidentID := event.EventID.String()

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
		responderID := event.EventID.String()
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
		if err := ctx.DB().Where("id = ?", event.EventID).Find(&comment).Error; err != nil {
			return nil, fmt.Errorf("error getting comment (id=%s)", event.EventID)
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
		if err := ctx.DB().Where("id = ?", event.EventID).Find(&evidence).Error; err != nil {
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
		componentID := event.EventID.String()

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
		configID := event.EventID.String()
		if event.Name == api.EventConfigChanged || event.Name == api.EventConfigUpdated {
			configID = event.Properties["config_id"]
		}

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

		if err := ctx.DB().Model(&models.ConfigChange{}).
			Select("change_type").
			Where("config_id = ?", configID).
			Where("severity IN ('low', 'medium', 'high')").
			Where("source NOT IN ('diff', 'config-db', 'notification', 'Playbook')").
			Where("created_at >= NOW() - INTERVAL '1 HOUR'").
			Group("change_type, severity").
			Order("CASE severity WHEN 'high' THEN 1 WHEN 'medium' THEN 2 WHEN 'low' THEN 3 ELSE 4 END").
			Limit(3).
			Find(&env.RecentEvents).Error; err != nil {
			return nil, fmt.Errorf("error finding recent changes for config(id=%s): %v", configID, err)
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

// getFirstSilencer returns the first matching silence that can silence notification on the given resource.
func getFirstSilencer(ctx context.Context, celEnv *celVariables, matchingSilences []models.NotificationSilence) *models.NotificationSilence {
	for _, silence := range matchingSilences {
		if silence.Filter == "" && silence.Selectors == nil {
			return &silence
		}

		if silence.Filter != "" {
			res, err := ctx.RunTemplate(gomplate.Template{Expression: string(silence.Filter)}, celEnv.AsMap(ctx))
			if err != nil {
				errMsg := fmt.Sprintf("filter evaluation failed for resource '%s': %v", celEnv.SelectableResource().GetID(), err)
				ctx.Errorf("silence %s (%q) failed: %s", silence.ID, lo.Ellipsis(string(silence.Filter), 30), errMsg)
				logs.IfError(db.UpdateNotificationSilenceError(ctx, silence.ID.String(), errMsg),
					fmt.Sprintf("failed to update notification silence(%s)", silence.ID))
				continue
			} else if ok, err := strconv.ParseBool(res); err != nil {
				errMsg := fmt.Sprintf("non-boolean result for resource '%s': %v", celEnv.SelectableResource().GetID(), err)
				ctx.Errorf("silence %q failed: %s", silence.Filter, errMsg)
				logs.IfError(db.UpdateNotificationSilenceError(ctx, silence.ID.String(), errMsg),
					fmt.Sprintf("failed to update notification silence(%s)", silence.ID))
				continue
			} else if ok {
				return &silence
			}
		}

		if silence.Selectors != nil {
			var resourceSelectors []types.ResourceSelector
			if err := json.Unmarshal(silence.Selectors, &resourceSelectors); err != nil {
				errMsg := fmt.Sprintf("failed to parse selectors for resource '%s': %v", celEnv.SelectableResource().GetID(), err)
				ctx.Errorf("silence %s (%s) failed: %s", silence.ID, lo.Ellipsis(string(silence.Selectors), 30), errMsg)
				logs.IfError(db.UpdateNotificationSilenceError(ctx, silence.ID.String(), errMsg),
					fmt.Sprintf("failed to update notification silence(%s)", silence.ID))
				continue
			}

			if matchSelectors(celEnv.SelectableResource(), resourceSelectors) {
				return &silence
			}
		}
	}

	return nil
}

func matchSelectors(selectableResource types.ResourceSelectable, resourceSelectors []types.ResourceSelector) bool {
	if selectableResource == nil {
		return false
	}

	for _, rs := range resourceSelectors {
		if rs.Matches(selectableResource) {
			return true
		}
	}

	return false
}
