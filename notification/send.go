package notification

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/flanksource/commons/collections"
	"github.com/flanksource/commons/utils"
	pkgConnection "github.com/flanksource/duty/connection"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/gomplate/v3"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/logs"
	"github.com/flanksource/incident-commander/teams"
)

// NotificationTemplate holds in data for notification
// that'll be used by struct templater.
type NotificationTemplate struct {
	Title      string            `template:"true"`
	Message    string            `template:"true"`
	Properties map[string]string `template:"true"`
}

const groupedResourcesMessage = `
Resources grouped with this notification:
{{- range .groupedResources }}
- {{ . }}
{{- end }}`

// DefaultTitleAndBody returns the default title and body for notification
// based on the given event using clicky-generated content.
func DefaultTitleAndBody(payload NotificationEventPayload, celEnv *celVariables) (title string, body string) {
	msgPayload := BuildNotificationMessagePayload(payload, celEnv)

	// For body, we generate both slack and non-slack versions via clicky
	// but since users with custom templates provide their own body,
	// we just return the plain text version as default
	bodyFormat := "markdown"
	if celEnv != nil && celEnv.Channel == "slack" {
		bodyFormat = "slack"
	}

	body, _ = FormatNotificationMessage(msgPayload, bodyFormat)

	return msgPayload.Title, body
}

// NotificationEventPayload holds data to create a notification.
type NotificationEventPayload struct {
	ID                        uuid.UUID     `json:"id"` // Resource id. depends what it is based on the original event.
	ResourceHealth            models.Health `json:"resource_health"`
	ResourceStatus            string        `json:"resource_status"`
	ResourceHealthDescription string        `json:"resource_health_description"`

	EventName      string     `json:"event_name"`                // The name of the original event this notification is for.
	NotificationID uuid.UUID  `json:"notification_id,omitempty"` // ID of the notification.
	EventCreatedAt time.Time  `json:"event_created_at"`          // Timestamp at which the original event was created
	Properties     []byte     `json:"properties,omitempty"`      // json encoded properties of the original event
	GroupID        *uuid.UUID `json:"group_id,omitempty"`        // ID of the group that the notification belongs to

	// Recipients //
	CustomService    *api.NotificationConfig `json:"custom_service,omitempty"`    // Send to connection or shoutrrr service
	Connection       *uuid.UUID              `json:"connection,omitempty"`        // Connection to use for the notification
	PlaybookID       *uuid.UUID              `json:"playbook_id,omitempty"`       // The playbook to trigger
	PersonID         *uuid.UUID              `json:"person_id,omitempty"`         // The person recipient.
	TeamID           *uuid.UUID              `json:"team_id,omitempty"`           // The team recipient.
	NotificationName string                  `json:"notification_name,omitempty"` // Name of the notification of a team
}

func (t *NotificationEventPayload) AsMap() map[string]string {
	// NOTE: Because the payload is marshalled to map[string]string instead of map[string]any
	// the custom_service field cannot be marshalled.
	// So, we marshal it separately and add it to the map.
	var customService string
	if t.CustomService != nil {
		b, _ := json.Marshal(t.CustomService)
		customService = string(b)
	}

	m := make(map[string]string)
	b, _ := json.Marshal(&t)
	_ = json.Unmarshal(b, &m)

	m["custom_service"] = customService
	return m
}

func (t *NotificationEventPayload) FromMap(m map[string]string) {
	b, _ := json.Marshal(m)
	_ = json.Unmarshal(b, &t)

	if customService, exists := m["custom_service"]; exists {
		_ = json.Unmarshal([]byte(customService), &t.CustomService)
	}
}

func storeNotificationPayload(ctx *Context, payload NotificationMessagePayload) {
	b, err := json.Marshal(payload)
	if err != nil {
		ctx.Logger.Warnf("failed to marshal notification payload: %v", err)
		return
	}
	ctx.WithBodyPayload(types.JSON(b))
}

type recipientSendFunc func(connectionName, shoutrrrURL string, properties map[string]string) error

func resolveRecipientAndSend(ctx *Context, payload NotificationEventPayload, celEnv *celVariables, notification *NotificationWithSpec, sendFn recipientSendFunc) error {
	if payload.PersonID != nil {
		ctx.WithRecipient(RecipientTypePerson, payload.PersonID)
		var emailAddress string
		if err := ctx.DB().Model(&models.Person{}).Select("email").Where("id = ?", *payload.PersonID).Find(&emailAddress).Error; err != nil {
			return fmt.Errorf("failed to get email of person(id=%s); %v", payload.PersonID, err)
		}

		smtpURL := fmt.Sprintf("%s?ToAddresses=%s", api.SystemSMTP, url.QueryEscape(emailAddress))
		return sendFn("", smtpURL, nil)
	}

	if payload.TeamID != nil {
		ctx.WithRecipient(RecipientTypeTeam, payload.TeamID)
		teamSpec, err := teams.GetTeamSpec(ctx.Context, payload.TeamID.String())
		if err != nil {
			return fmt.Errorf("failed to get team(id=%s); %v", payload.TeamID, err)
		}

		for _, cn := range teamSpec.Notifications {
			if cn.Name != payload.NotificationName {
				continue
			}

			if cn.Webhook != nil {
				ctx.WithRecipient(RecipientTypeWebhook, nil)
				return sendWebhookNotification(ctx, celEnv, payload, cn.Webhook, notification)
			}

			return sendFn(cn.Connection, cn.URL, cn.Properties)
		}

		return fmt.Errorf("notification %q not found in team(id=%s) spec", payload.NotificationName, payload.TeamID)
	}

	if payload.CustomService != nil {
		cn := payload.CustomService
		if cn.Webhook != nil {
			ctx.WithRecipient(RecipientTypeWebhook, nil)
			return sendWebhookNotification(ctx, celEnv, payload, cn.Webhook, notification)
		}
		ctx.WithRecipient(RecipientTypeURL, nil)
		return sendFn(cn.Connection, cn.URL, cn.Properties)
	}

	return fmt.Errorf("no recipient resolved for notification(id=%s) event=%s", payload.NotificationID, payload.EventName)
}

// PrepareAndSendEventNotification generates the notification from the given event and sends it.
func PrepareAndSendEventNotification(ctx *Context, payload NotificationEventPayload, celEnv *celVariables) error {
	notification, err := GetNotification(ctx.Context, payload.NotificationID.String())
	if err != nil {
		return err
	}

	if strings.TrimSpace(notification.Template) != "" {
		return prepareAndSendRawNotification(ctx, payload, celEnv, notification)
	}

	msgPayload := BuildNotificationMessagePayload(payload, celEnv)
	applyTemplateOverrides(ctx, &msgPayload, notification, celEnv)
	storeNotificationPayload(ctx, msgPayload)

	return resolveRecipientAndSend(ctx, payload, celEnv, notification, func(connectionName, shoutrrrURL string, properties map[string]string) error {
		return sendEventNotificationWithMetrics(ctx, msgPayload, celEnv, connectionName, shoutrrrURL, notification, properties)
	})
}

func prepareAndSendRawNotification(ctx *Context, payload NotificationEventPayload, celEnv *celVariables, notification *NotificationWithSpec) error {
	return resolveRecipientAndSend(ctx, payload, celEnv, notification, func(connectionName, shoutrrrURL string, properties map[string]string) error {
		return sendRawEventNotificationWithMetrics(ctx, payload, celEnv, connectionName, shoutrrrURL, notification, properties)
	})
}

// triggerPlaybookRun creates an event to trigger a playbook run.
// The notification of the status is then handled entirely by playbook.
func triggerPlaybookRun(ctx *Context, celEnv *celVariables, playbookID uuid.UUID) error {
	ctx.WithRecipient(RecipientTypePlaybook, nil) // the id is populated later by playbook when a run is triggered

	err := ctx.Transaction(func(txCtx context.Context, _ trace.Span) error {
		eventProp := types.JSONStringMap{
			"id":                       playbookID.String(),
			"notification_id":          ctx.notificationID.String(),
			"notification_dispatch_id": ctx.log.ID.String(),
		}

		switch {
		case celEnv.ConfigItem != nil:
			eventProp["config_id"] = celEnv.ConfigItem.ID.String()
		case celEnv.Component != nil:
			eventProp["component_id"] = celEnv.Component.ID.String()
		case celEnv.Check != nil:
			eventProp["check_id"] = celEnv.Check.ID.String()
		}

		event := models.Event{
			Name:       api.EventPlaybookRun,
			Properties: eventProp,
		}
		if err := txCtx.DB().Create(&event).Error; err != nil {
			return fmt.Errorf("failed to create run: %w", err)
		}

		return nil
	})
	if err != nil {
		notificationSendFailureCounter.WithLabelValues("playbook", string(RecipientTypePlaybook), ctx.notificationID.String()).Inc()
		return err
	}

	// notificationSentCounter.WithLabelValues("playbook", string(RecipientTypePlaybook), ctx.notificationID.String()).Inc()
	// notificationSendDuration.WithLabelValues("playbook", string(RecipientTypePlaybook), ctx.notificationID.String()).Observe(time.Since(start).Seconds())

	return nil
}

// SendRawEventNotification is a wrapper around sendRawEventNotification() for better error handling & metrics collection purpose.
func sendRawEventNotificationWithMetrics(ctx *Context, payload NotificationEventPayload, celEnv *celVariables, connectionName, shoutrrrURL string, notification *NotificationWithSpec, customProperties map[string]string) error {
	start := time.Now()

	service, err := sendRawEventNotification(ctx, payload, celEnv, connectionName, shoutrrrURL, notification, customProperties)
	if err != nil {
		notificationSendFailureCounter.WithLabelValues(service, string(ctx.recipientType), ctx.notificationID.String()).Inc()
		return err
	}

	notificationSentCounter.WithLabelValues(service, string(ctx.recipientType), ctx.notificationID.String()).Inc()
	notificationSendDuration.WithLabelValues(service, string(ctx.recipientType), ctx.notificationID.String()).Observe(time.Since(start).Seconds())

	return nil
}

func sendRawEventNotification(ctx *Context, payload NotificationEventPayload, celEnv *celVariables, connectionName, shoutrrrURL string, notification *NotificationWithSpec, customProperties map[string]string) (string, error) {
	defaultTitle, defaultBody := DefaultTitleAndBody(payload, celEnv)
	customProperties = collections.MergeMap(notification.Properties, customProperties)
	data := NotificationTemplate{
		Title:      utils.Coalesce(notification.Title, defaultTitle),
		Message:    utils.Coalesce(notification.Template, defaultBody),
		Properties: customProperties,
	}

	service, err := SendRawNotification(ctx, connectionName, shoutrrrURL, celEnv.AsMap(ctx.Context), data, notification)
	if err != nil {
		return service, err
	}
	if CRDStatusUpdateQueue != nil && notification != nil && notification.Source == models.SourceCRD {
		CRDStatusUpdateQueue.EnqueueWithDelay(notification.ID.String(), 30*time.Second)
	}
	return service, nil
}

// SendEventNotification is a wrapper around sendEventNotification() for better error handling & metrics collection purpose.
func sendEventNotificationWithMetrics(ctx *Context, payload NotificationMessagePayload, celEnv *celVariables, connectionName, shoutrrrURL string, notification *NotificationWithSpec, customProperties map[string]string) error {
	start := time.Now()

	service, err := sendEventNotification(ctx, payload, celEnv, connectionName, shoutrrrURL, notification, customProperties)
	if err != nil {
		notificationSendFailureCounter.WithLabelValues(service, string(ctx.recipientType), ctx.notificationID.String()).Inc()
		return err
	}

	notificationSentCounter.WithLabelValues(service, string(ctx.recipientType), ctx.notificationID.String()).Inc()
	notificationSendDuration.WithLabelValues(service, string(ctx.recipientType), ctx.notificationID.String()).Observe(time.Since(start).Seconds())

	return nil
}

func sendEventNotification(ctx *Context, payload NotificationMessagePayload, celEnv *celVariables, connectionName, shoutrrrURL string, notification *NotificationWithSpec, customProperties map[string]string) (string, error) {
	customProperties = collections.MergeMap(notification.Properties, customProperties)
	service, err := SendNotification(ctx, connectionName, shoutrrrURL, payload, customProperties, celEnv)
	if err != nil {
		return service, err
	}
	if CRDStatusUpdateQueue != nil && notification != nil && notification.Source == models.SourceCRD {
		CRDStatusUpdateQueue.EnqueueWithDelay(notification.ID.String(), 30*time.Second)
	}
	return service, nil
}

func SendRawNotification(ctx *Context, connectionName, shoutrrrURL string, celEnv map[string]any, data NotificationTemplate, notification *NotificationWithSpec) (string, error) {
	if celEnv == nil {
		celEnv = make(map[string]any)
	}

	var connection *models.Connection
	var err error
	if connectionName != "" {
		connection, err = pkgConnection.Get(ctx.Context, connectionName)
		if err != nil {
			return "", err
		}

		ctx.WithRecipient(RecipientTypeConnection, &connection.ID)

		shoutrrrURL = connection.URL
		data.Properties = collections.MergeMap(connection.Properties, data.Properties)
	}

	if connection != nil && connection.Type == models.ConnectionTypeSlack {
		celEnv["channel"] = "slack"
		templater := ctx.NewStructTemplater(celEnv, "", TemplateFuncs)
		if err := templater.Walk(&data); err != nil {
			return "", fmt.Errorf("error templating notification: %w", err)
		}

		ctx.WithMessage(data.Message)
		resourceID := ""
		if ctx.log != nil && ctx.log.ResourceID != uuid.Nil {
			resourceID = ctx.log.ResourceID.String()
		}
		traceLog("NotificationID=%s Resource=[%s] Sent via slack ...", ctx.notificationID, resourceID)
		if err := SlackSend(ctx, connection.Password, connection.Username, data); err != nil {
			return "", err
		}

		return "slack", nil
	}

	if _, exists := celEnv["groupedResources"]; exists {
		data.Message += groupedResourcesMessage
	}

	service, err := shoutrrrSendRaw(ctx, celEnv, shoutrrrURL, data)
	if err != nil {
		return "", fmt.Errorf("failed to send message with Shoutrrr: %w", err)
	}
	resourceID := ""
	if ctx.log != nil && ctx.log.ResourceID != uuid.Nil {
		resourceID = ctx.log.ResourceID.String()
	}
	traceLog("NotificationID=%s Resource=[%s] Sent via Shoutrrr ...", ctx.notificationID, resourceID)

	return service, nil
}

func SendNotification(ctx *Context, connectionName, shoutrrrURL string, payload NotificationMessagePayload, properties map[string]string, celEnv *celVariables) (string, error) {
	var connection *models.Connection
	var err error
	if connectionName != "" {
		connection, err = pkgConnection.Get(ctx.Context, connectionName)
		if err != nil {
			return "", err
		}

		ctx.WithRecipient(RecipientTypeConnection, &connection.ID)

		shoutrrrURL = connection.URL
		properties = collections.MergeMap(connection.Properties, properties)
	}

	// Template render properties if celEnv is available
	if celEnv != nil {
		properties = renderTemplateProperties(ctx, properties, celEnv)
	}

	if connection != nil && connection.Type == models.ConnectionTypeSlack {
		slackMsg, err := FormatNotificationMessage(payload, "slack")
		if err != nil {
			return "", fmt.Errorf("failed to format slack message: %w", err)
		}

		data := NotificationTemplate{
			Title:      payload.Title,
			Message:    slackMsg,
			Properties: properties,
		}

		resourceID := ""
		if ctx.log != nil && ctx.log.ResourceID != uuid.Nil {
			resourceID = ctx.log.ResourceID.String()
		}
		traceLog("NotificationID=%s Resource=[%s] Sent via slack ...", ctx.notificationID, resourceID)
		if err := SlackSend(ctx, connection.Password, connection.Username, data); err != nil {
			return "", err
		}

		return "slack", nil
	}

	service, err := shoutrrrSend(ctx, shoutrrrURL, payload, properties)
	if err != nil {
		return "", fmt.Errorf("failed to send message with Shoutrrr: %w", err)
	}
	resourceID := ""
	if ctx.log != nil && ctx.log.ResourceID != uuid.Nil {
		resourceID = ctx.log.ResourceID.String()
	}
	traceLog("NotificationID=%s Resource=[%s] Sent via Shoutrrr ...", ctx.notificationID, resourceID)

	return service, nil
}

func CreateNotificationSendPayloads(ctx context.Context, event models.Event, n *NotificationWithSpec, celEnv *celVariables) ([]NotificationEventPayload, error) {
	celEnvMap := celEnv.AsMap(ctx)

	var payloads []NotificationEventPayload

	resourceID, err := uuid.Parse(event.Properties["id"])
	if err != nil {
		return nil, fmt.Errorf("failed to parse resource id: %v", err)
	}

	var eventProperties []byte
	if len(event.Properties) > 0 {
		eventProperties, err = json.Marshal(event.Properties)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal event properties: %v", err)
		}
	}

	var groupID *uuid.UUID
	if len(n.GroupBy) > 0 {
		groupByHash, err := calculateGroupByHash(ctx, n.GroupBy, resourceID.String(), event.Name)
		if err != nil {
			return nil, err
		}

		groupByInterval := n.GroupByInterval
		if groupByInterval == 0 {
			groupByInterval = ctx.Properties().Duration("notifications.group_by_interval", DefaultGroupByInterval)
		}

		group, err := db.AddResourceToGroup(ctx, groupByInterval, groupByHash, n.ID, &resourceID, nil, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to add resource to group: %w", err)
		} else if group != nil {
			groupID = &group.ID
		}
	}

	resource := celEnv.SelectableResource()
	var resourceHealth, resourceStatus, resourceHealthDescription string
	if resource != nil {
		var err error
		resourceHealth, err = resource.GetHealth()
		if err != nil {
			return nil, fmt.Errorf("failed to get resource health: %w", err)
		}

		resourceStatus, err = resource.GetStatus()
		if err != nil {
			return nil, fmt.Errorf("failed to get resource status: %w", err)
		}

		if dd, ok := resource.(types.DescriptionProvider); ok {
			resourceHealthDescription = dd.GetHealthDescription()
		}
	}

	if n.PlaybookID != nil {
		payload := NotificationEventPayload{
			EventName:                 event.Name,
			NotificationID:            n.ID,
			ResourceHealth:            models.Health(resourceHealth),
			ResourceStatus:            resourceStatus,
			ResourceHealthDescription: resourceHealthDescription,
			ID:                        resourceID,
			PlaybookID:                n.PlaybookID,
			EventCreatedAt:            event.CreatedAt,
			Properties:                eventProperties,
			GroupID:                   groupID,
		}

		payloads = append(payloads, payload)
	}

	if n.PersonID != nil {
		payload := NotificationEventPayload{
			EventName:                 event.Name,
			NotificationID:            n.ID,
			ResourceHealth:            models.Health(resourceHealth),
			ResourceHealthDescription: resourceHealthDescription,
			ResourceStatus:            resourceStatus,
			ID:                        resourceID,
			PersonID:                  n.PersonID,
			EventCreatedAt:            event.CreatedAt,
			Properties:                eventProperties,
			GroupID:                   groupID,
		}

		payloads = append(payloads, payload)
	}

	if n.TeamID != nil {
		teamSpec, err := teams.GetTeamSpec(ctx, n.TeamID.String())
		if err != nil {
			return nil, fmt.Errorf("failed to get team (id=%s); %v", n.TeamID, err)
		}

		for _, cn := range teamSpec.Notifications {
			if cn.Filter != "" {
				if valid, err := ctx.RunTemplateBool(gomplate.Template{Expression: cn.Filter}, celEnvMap); err != nil {
					logs.IfError(db.SetNotificationError(ctx, n.ID.String(), err.Error()), "failed to update notification")
					continue
				} else if !valid {
					continue
				}
			}

			payload := NotificationEventPayload{
				EventName:                 event.Name,
				NotificationID:            n.ID,
				ResourceHealth:            models.Health(resourceHealth),
				ResourceHealthDescription: resourceHealthDescription,
				ResourceStatus:            resourceStatus,
				ID:                        resourceID,
				TeamID:                    n.TeamID,
				NotificationName:          cn.Name,
				EventCreatedAt:            event.CreatedAt,
				Properties:                eventProperties,
				GroupID:                   groupID,
			}

			payloads = append(payloads, payload)
		}
	}

	for _, cn := range n.CustomNotifications {
		if cn.Filter != "" {
			if valid, err := ctx.RunTemplateBool(gomplate.Template{Expression: cn.Filter}, celEnvMap); err != nil {
				logs.IfError(db.SetNotificationError(ctx, n.ID.String(), err.Error()), "failed to update notification")
				continue
			} else if !valid {
				continue
			}
		}

		payload := NotificationEventPayload{
			EventName:                 event.Name,
			NotificationID:            n.ID,
			ResourceHealth:            models.Health(resourceHealth),
			ResourceHealthDescription: resourceHealthDescription,
			ResourceStatus:            resourceStatus,
			CustomService:             cn.DeepCopy(),
			ID:                        resourceID,
			EventCreatedAt:            event.CreatedAt,
			Properties:                eventProperties,
			GroupID:                   groupID,
		}

		if cn.Connection != "" {
			c, err := pkgConnection.Get(ctx, cn.Connection)
			if err != nil {
				return nil, fmt.Errorf("failed to get connection: %w", err)
			}
			celEnvMap["channel"] = c.Type
			payload.Connection = &c.ID
		}

		payloads = append(payloads, payload)
	}

	return payloads, nil
}

// applyTemplateOverrides replaces the payload title/description with rendered templates when provided.
func applyTemplateOverrides(ctx *Context, msgPayload *NotificationMessagePayload, notification *NotificationWithSpec, celEnv *celVariables) {
	if msgPayload == nil || notification == nil || celEnv == nil {
		return
	}

	celEnvMap := celEnv.AsMap(ctx.Context)
	if strings.TrimSpace(notification.Title) != "" {
		if rendered, err := renderTemplateString(ctx, celEnvMap, notification.Title); err != nil {
			ctx.Logger.Warnf("failed to render notification title template %q: %v", notification.Title, err)
		} else {
			msgPayload.Title = rendered
		}
	}

	if strings.TrimSpace(notification.Template) != "" {
		if rendered, err := renderTemplateString(ctx, celEnvMap, notification.Template); err != nil {
			ctx.Logger.Warnf("failed to render notification template %q: %v", notification.Template, err)
		} else {
			msgPayload.Description = rendered
		}
	}
}

// renderTemplateProperties renders template variables in the properties map using the CEL environment
func renderTemplateProperties(ctx *Context, properties map[string]string, celEnv *celVariables) map[string]string {
	if len(properties) == 0 {
		return properties
	}

	rendered := make(map[string]string, len(properties))
	celEnvMap := celEnv.AsMap(ctx.Context)

	for key, value := range properties {
		// Skip properties that don't contain template syntax
		if !strings.Contains(value, "{{") || !strings.Contains(value, "}}") {
			rendered[key] = value
			continue
		}

		renderedValue, err := renderTemplateString(ctx, celEnvMap, value)
		if err != nil {
			ctx.Logger.Warnf("failed to render template property %s=%s: %v", key, value, err)
			rendered[key] = value
			continue
		}
		rendered[key] = renderedValue
	}

	return rendered
}

func renderTemplateString(ctx *Context, celEnvMap map[string]any, value string) (string, error) {
	result, err := ctx.RunTemplate(gomplate.Template{Template: value}, celEnvMap)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%v", result), nil
}
