package notification

import (
	"embed"
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
	"github.com/samber/lo"
	"go.opentelemetry.io/otel/trace"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/logs"
	"github.com/flanksource/incident-commander/teams"
)

//go:embed templates/*
var templates embed.FS

const groupedResourcesMessage = `
Resources grouped with this notification:
{{- range .groupedResources }}
- {{ . }}
{{- end }}`

// NotificationTemplate holds in data for notification
// that'll be used by struct templater.
type NotificationTemplate struct {
	Title      string            `template:"true"`
	Message    string            `template:"true"`
	Properties map[string]string `template:"true"`
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
	Body           *string    `json:"-"`                         // Body of the notification

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

// PrepareAndSendEventNotification generates the notification from the given event and sends it.
func PrepareAndSendEventNotification(ctx *Context, payload NotificationEventPayload, celEnv *celVariables) error {
	notification, err := GetNotification(ctx.Context, payload.NotificationID.String())
	if err != nil {
		return err
	}

	if payload.PersonID != nil {
		ctx.WithRecipient(RecipientTypePerson, payload.PersonID)
		var emailAddress string
		if err := ctx.DB().Model(&models.Person{}).Select("email").Where("id = ?", *payload.PersonID).Find(&emailAddress).Error; err != nil {
			return fmt.Errorf("failed to get email of person(id=%s); %v", payload.PersonID, err)
		}

		smtpURL := fmt.Sprintf("%s?ToAddresses=%s", api.SystemSMTP, url.QueryEscape(emailAddress))
		return sendEventNotificationWithMetrics(ctx, celEnv.AsMap(ctx.Context), "", smtpURL, payload.EventName, notification, nil)
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

			return sendEventNotificationWithMetrics(ctx, celEnv.AsMap(ctx.Context), cn.Connection, cn.URL, payload.EventName, notification, cn.Properties)
		}
	}

	if payload.CustomService != nil {
		cn := payload.CustomService
		ctx.WithRecipient(RecipientTypeURL, nil)
		return sendEventNotificationWithMetrics(ctx, celEnv.AsMap(ctx.Context), cn.Connection, cn.URL, payload.EventName, notification, cn.Properties)
	}

	return nil
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

// SendEventNotification is a wrapper around sendEventNotification() for better error handling & metrics collection purpose.
func sendEventNotificationWithMetrics(ctx *Context, celEnv map[string]any, connectionName, shoutrrrURL, eventName string, notification *NotificationWithSpec, customProperties map[string]string) error {
	start := time.Now()

	service, err := sendEventNotification(ctx, celEnv, connectionName, shoutrrrURL, eventName, notification, customProperties)
	if err != nil {
		notificationSendFailureCounter.WithLabelValues(service, string(ctx.recipientType), ctx.notificationID.String()).Inc()
		return err
	}

	notificationSentCounter.WithLabelValues(service, string(ctx.recipientType), ctx.notificationID.String()).Inc()
	notificationSendDuration.WithLabelValues(service, string(ctx.recipientType), ctx.notificationID.String()).Observe(time.Since(start).Seconds())

	return nil
}

func sendEventNotification(ctx *Context, celEnv map[string]any, connectionName, shoutrrrURL, eventName string, notification *NotificationWithSpec, customProperties map[string]string) (string, error) {
	defaultTitle, defaultBody := DefaultTitleAndBody(eventName)
	customProperties = collections.MergeMap(notification.Properties, customProperties)
	data := NotificationTemplate{
		Title:      utils.Coalesce(notification.Title, defaultTitle),
		Message:    utils.Coalesce(notification.Template, defaultBody),
		Properties: customProperties,
	}

	return SendNotification(ctx, connectionName, shoutrrrURL, celEnv, data, notification)
}

func SendNotification(ctx *Context, connectionName, shoutrrrURL string, celEnv map[string]any, data NotificationTemplate, notification *NotificationWithSpec) (string, error) {
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
		// We know we are sending to slack.
		// Send the notification with slack-api and don't go through Shoutrrr.
		celEnv["channel"] = "slack"
		templater := ctx.NewStructTemplater(celEnv, "", TemplateFuncs)
		if err := templater.Walk(&data); err != nil {
			return "", fmt.Errorf("error templating notification: %w", err)
		}

		traceLog("NotificationID=%s Resource=[%s] Sent via slack ...", lo.FromPtr(notification).ID, getResourceIDFromCELMap(celEnv))
		if err := SlackSend(ctx, connection.Password, connection.Username, data); err != nil {
			return "", err
		}

		return "slack", nil
	}

	if _, exists := celEnv["groupedResources"]; exists {
		data.Message += groupedResourcesMessage
	}

	service, err := shoutrrrSend(ctx, celEnv, shoutrrrURL, data)
	if err != nil {
		return "", fmt.Errorf("failed to send message with Shoutrrr: %w", err)
	}
	traceLog("NotificationID=%s Resource=[%s] Sent via Shoutrrr ...", lo.FromPtr(notification).ID, getResourceIDFromCELMap(celEnv))

	// Update CRD Status
	if CRDStatusUpdateQueue != nil && notification != nil && notification.Source == models.SourceCRD {
		CRDStatusUpdateQueue.EnqueueWithDelay(notification.ID.String(), 30*time.Second)
	}

	return service, nil
}

// DefaultTitleAndBody returns the default title and body for notification
// based on the given event.
func DefaultTitleAndBody(event string) (title string, body string) {
	switch event {
	case "notification.watchdog":
		// A dummy event that lives only within the application (never in the event queue)
		title = `{{ if ne channel "slack"}}Notification {{.summary.namespace}}/{{.summary.name}} Summary{{end}}`
		content, _ := templates.ReadFile(fmt.Sprintf("templates/%s", event))
		body = string(content)

	case api.EventCheckPassed:
		title = `{{ if ne channel "slack"}}Check {{.check.name}} has passed{{end}}`
		content, _ := templates.ReadFile(fmt.Sprintf("templates/%s", event))
		body = string(content)

	case api.EventCheckFailed:
		title = `{{ if ne channel "slack"}}Check {{.check.name}} has failed{{end}}`
		content, _ := templates.ReadFile(fmt.Sprintf("templates/%s", event))
		body = string(content)

	case api.EventConfigHealthy, api.EventConfigUnhealthy, api.EventConfigWarning, api.EventConfigUnknown, api.EventConfigDegraded:
		title = `{{ if ne channel "slack"}}{{.config.type}} {{.config.name}} is {{.config.health}}{{end}}`
		content, _ := templates.ReadFile("templates/config.health")
		body = string(content)

	case api.EventConfigCreated, api.EventConfigUpdated, api.EventConfigDeleted, api.EventConfigChanged:
		title = fmt.Sprintf(`{{ if ne channel "slack"}}{{.config.type}} {{.config.name}} was %s{{end}}`, strings.TrimPrefix(event, "config."))
		content, _ := templates.ReadFile("templates/config.db.update")
		body = string(content)

	case api.EventComponentHealthy, api.EventComponentUnhealthy, api.EventComponentWarning, api.EventComponentUnknown:
		title = `{{ if ne channel "slack"}}Component {{.component.name}} is {{.component.health}}{{end}}`
		content, _ := templates.ReadFile("templates/component.health")
		body = string(content)

	case api.EventIncidentCommentAdded:
		title = "{{.author.name}} left a comment on {{.incident.incident_id}}: {{.incident.title}}"
		body = "{{.comment.comment}}\n\n[Reference]({{.permalink}})"

	case api.EventIncidentCreated:
		title = "{{.incident.incident_id}}: {{.incident.title}} ({{.incident.severity}}) created"
		body = "Type: {{.incident.type}}\n\n[Reference]({{.permalink}})"

	case api.EventIncidentDODAdded:
		title = "Definition of Done added | {{.incident.incident_id}}: {{.incident.title}}"
		body = "Evidence: {{.evidence.description}}\n\n[Reference]({{.permalink}})"

	case api.EventIncidentDODPassed, api.EventIncidentDODRegressed:
		title = "Definition of Done {{if .evidence.done}}passed{{else}}regressed{{end}} | {{.incident.incident_id}}: {{.incident.title}}"
		body = `Evidence: {{.evidence.description}}
Hypothesis: {{.hypothesis.title}}

[Reference]({{.permalink}})`

	case api.EventIncidentResponderAdded:
		title = "New responder added to {{.incident.incident_id}}: {{.incident.title}}"
		body = "Responder {{.responder.name}}\n\n[Reference]({{.permalink}})"

	case api.EventIncidentResponderRemoved:
		title = "Responder removed from {{.incident.incident_id}}: {{.incident.title}}"
		body = "Responder {{.responder.name}}\n\n[Reference]({{.permalink}})"

	case api.EventIncidentStatusCancelled, api.EventIncidentStatusClosed, api.EventIncidentStatusInvestigating, api.EventIncidentStatusMitigated, api.EventIncidentStatusOpen, api.EventIncidentStatusResolved:
		title = "{{.incident.title}} status updated"
		body = "New Status: {{.incident.status}}\n\n[Reference]({{.permalink}})"
	}

	return title, body
}

func getNotificationMsg(ctx context.Context, celEnv map[string]any, payload NotificationEventPayload, n *NotificationWithSpec) (*NotificationTemplate, error) {
	defaultTitle, defaultBody := DefaultTitleAndBody(payload.EventName)
	data := NotificationTemplate{
		Title:      utils.Coalesce(n.Title, defaultTitle),
		Message:    utils.Coalesce(n.Template, defaultBody),
		Properties: n.Properties,
	}
	templater := ctx.NewStructTemplater(celEnv, "", TemplateFuncs)
	if err := templater.Walk(&data); err != nil {
		return nil, fmt.Errorf("error templating notification: %w", err)
	}

	if strings.Contains(data.Message, `"blocks"`) {
		var slackMsg SlackMsgTemplate
		if err := json.Unmarshal([]byte(data.Message), &slackMsg); err != nil {
			return nil, fmt.Errorf("failed to unmarshal slack template into blocks: %w", err)
		}

		if b, err := json.Marshal([]any{slackMsg}); err == nil {
			data.Message = string(b)
		}
	}

	return &data, nil
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

		msg, err := getNotificationMsg(ctx, celEnvMap, payload, n)
		if err != nil {
			return nil, fmt.Errorf("failed to get notification body: %w", err)
		}

		payload.Body = &msg.Message

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

		msg, err := getNotificationMsg(ctx, celEnvMap, payload, n)
		if err != nil {
			return nil, fmt.Errorf("failed to get notification body: %w", err)
		}
		payload.Body = &msg.Message

		payloads = append(payloads, payload)
	}

	return payloads, nil
}

func getResourceIDFromCELMap(celEnv map[string]any) string {
	if c, exists := celEnv["check"]; exists {
		if cm, ok := c.(map[string]any); ok {
			return "check/" + fmt.Sprint(cm["id"])
		}
	}
	if c, exists := celEnv["config"]; exists {
		if cm, ok := c.(map[string]any); ok {
			return "config/" + fmt.Sprint(cm["id"])
		}
	}
	if c, exists := celEnv["component"]; exists {
		if cm, ok := c.(map[string]any); ok {
			return "component/" + fmt.Sprint(cm["id"])
		}
	}
	return ""
}
