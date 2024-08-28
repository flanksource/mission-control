package notification

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/flanksource/commons/collections"
	"github.com/flanksource/commons/utils"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"

	"github.com/flanksource/incident-commander/api"

	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/logs"
	"github.com/flanksource/incident-commander/teams"
	"github.com/flanksource/incident-commander/utils/expression"
)

// List of all possible variables for any expression related to notifications
var allEnvVars = []string{"agent", "config", "check", "canary", "component", "incident", "team", "responder", "comment", "evidence", "hypothesis", "permalink"}

// NotificationTemplate holds in data for notification
// that'll be used by struct templater.
type NotificationTemplate struct {
	Title      string            `template:"true"`
	Message    string            `template:"true"`
	Properties map[string]string `template:"true"`
}

// NotificationEventPayload holds data to create a notification.
type NotificationEventPayload struct {
	ID               uuid.UUID  `json:"id"`                          // Resource id. depends what it is based on the original event.
	EventName        string     `json:"event_name"`                  // The name of the original event this notification is for.
	PersonID         *uuid.UUID `json:"person_id,omitempty"`         // The person recipient.
	TeamID           string     `json:"team_id,omitempty"`           // The team recipient.
	NotificationName string     `json:"notification_name,omitempty"` // Name of the notification of a team
	NotificationID   uuid.UUID  `json:"notification_id,omitempty"`   // ID of the notification.
	EventCreatedAt   time.Time  `json:"event_created_at"`            // Timestamp at which the original event was created
	Properties       []byte     `json:"properties,omitempty"`        // json encoded properties of the original event
}

func (t *NotificationEventPayload) AsMap() map[string]string {
	m := make(map[string]string)
	b, _ := json.Marshal(&t)
	_ = json.Unmarshal(b, &m)
	return m
}

func (t *NotificationEventPayload) FromMap(m map[string]string) {
	b, _ := json.Marshal(m)
	_ = json.Unmarshal(b, &t)
}

// PrepareAndSendEventNotification generates the notification from the given event and sends it.
func PrepareAndSendEventNotification(ctx *Context, payload NotificationEventPayload, celEnv map[string]any) error {
	notification, err := GetNotification(ctx.Context, payload.NotificationID.String())
	if err != nil {
		return err
	}

	if payload.PersonID != nil {
		ctx.WithPersonID(payload.PersonID).WithRecipientType(RecipientTypePerson)
		var emailAddress string
		if err := ctx.DB().Model(&models.Person{}).Select("email").Where("id = ?", payload.PersonID).Find(&emailAddress).Error; err != nil {
			return fmt.Errorf("failed to get email of person(id=%s); %v", payload.PersonID, err)
		}

		smtpURL := fmt.Sprintf("%s?ToAddresses=%s", api.SystemSMTP, url.QueryEscape(emailAddress))
		return sendEventNotificationWithMetrics(ctx, celEnv, "", smtpURL, payload.EventName, notification, nil)
	}

	if payload.TeamID != "" {
		ctx.WithRecipientType(RecipientTypeTeam)
		teamSpec, err := teams.GetTeamSpec(ctx.Context, payload.TeamID)
		if err != nil {
			return fmt.Errorf("failed to get team(id=%s); %v", payload.TeamID, err)
		}

		for _, cn := range teamSpec.Notifications {
			if cn.Name != payload.NotificationName {
				continue
			}

			return sendEventNotificationWithMetrics(ctx, celEnv, cn.Connection, cn.URL, payload.EventName, notification, cn.Properties)
		}
	}

	// CustomNotifications, even though it's a slice,
	// contains only a single notification.
	// It's a slice for backward compatibility reasons.
	// nolint: staticcheck
	// (SA4004: the surrounding loop is unconditionally terminated)
	for _, cn := range notification.CustomNotifications {
		ctx.WithRecipientType(RecipientTypeCustom)
		return sendEventNotificationWithMetrics(ctx, celEnv, cn.Connection, cn.URL, payload.EventName, notification, cn.Properties)
	}

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
	defaultTitle, defaultBody := defaultTitleAndBody(eventName)
	customProperties = collections.MergeMap(notification.Properties, customProperties)
	data := NotificationTemplate{
		Title:      utils.Coalesce(notification.Title, defaultTitle),
		Message:    utils.Coalesce(notification.Template, defaultBody),
		Properties: customProperties,
	}

	return SendNotification(ctx, connectionName, shoutrrrURL, celEnv, data)
}

func SendNotification(ctx *Context, connectionName, shoutrrrURL string, celEnv map[string]any, data NotificationTemplate) (string, error) {
	if celEnv == nil {
		celEnv = make(map[string]any)
	}

	var connection *models.Connection
	var err error
	if connectionName != "" {
		connection, err = ctx.HydrateConnectionByURL(connectionName)
		if err != nil {
			return "", err
		} else if connection == nil {
			return "", fmt.Errorf("connection (%s) not found", connectionName)
		}

		shoutrrrURL = connection.URL
		data.Properties = collections.MergeMap(connection.Properties, data.Properties)
	}

	if connection != nil && connection.Type == models.ConnectionTypeSlack {
		// We know we are sending to slack.
		// Send the notification with slack-api and don't go through Shoutrrr.
		celEnv["outgoing_channel"] = "slack"
		templater := ctx.NewStructTemplater(celEnv, "", nil)
		if err := templater.Walk(&data); err != nil {
			return "", fmt.Errorf("error templating notification: %w", err)
		}
		return "slack", SlackSend(ctx, connection.Password, connection.Username, data)
	}

	service, err := shoutrrrSend(ctx, nil, shoutrrrURL, data)
	if err != nil {
		return "", fmt.Errorf("failed to send message with Shoutrrr: %w", err)
	}

	return service, nil
}

// labelsTemplate is a helper func to generate the template for displaying labels
func labelsTemplate(field string) string {
	return fmt.Sprintf("{{if %s}}### Labels: \n{{range $k, $v := %s}}**{{$k}}**: {{$v}} \n{{end}}{{end}}", field, field)
}

// defaultTitleAndBody returns the default title and body for notification
// based on the given event.
func defaultTitleAndBody(event string) (title string, body string) {
	switch event {
	case api.EventCheckPassed:
		title = "Check {{.check.name}} has passed"
		body = fmt.Sprintf(`{{ if eq outgoing_channel "slack"}}
Sending From Slack
{{ else }}
Canary: {{.canary.name}}
{{if .agent}}Agent: {{.agent.name}}{{end}}
{{if .status.message}}Message: {{.status.message}} {{end}}
%s

[Reference]({{.permalink}})
{{end}}`, labelsTemplate(".check.labels"))

	case api.EventCheckFailed:
		title = "Check {{.check.name}} has failed"
		body = fmt.Sprintf(`Canary: {{.canary.name}}
{{if .agent}}Agent: {{.agent.name}}{{end}}
Error: {{.status.error}}
%s

[Reference]({{.permalink}})`, labelsTemplate(".check.labels"))

	case api.EventConfigHealthy, api.EventConfigUnhealthy, api.EventConfigWarning, api.EventConfigUnknown:
		title = "{{.config.type}} {{.config.name}} is {{.config.health}}"
		body = fmt.Sprintf("%s\n[Reference]({{.permalink}})", labelsTemplate(".config.labels"))

	case api.EventConfigCreated, api.EventConfigUpdated, api.EventConfigDeleted:
		title = fmt.Sprintf("{{.config.type}} {{.config.name}} was %s", strings.TrimPrefix(event, "config."))
		body = fmt.Sprintf("%s\n[Reference]({{.permalink}})", labelsTemplate(".config.labels"))

	case api.EventComponentHealthy, api.EventComponentUnhealthy, api.EventComponentWarning, api.EventComponentUnknown:
		title = "Component {{.component.name}} is {{.component.health}}"
		body = fmt.Sprintf("%s\n[Reference]({{.permalink}})", labelsTemplate(".component.labels"))

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

func CreateNotificationSendPayloads(ctx context.Context, event models.Event, n *NotificationWithSpec, celEnv map[string]any) ([]NotificationEventPayload, error) {
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

	if n.PersonID != nil {
		payload := NotificationEventPayload{
			EventName:      event.Name,
			NotificationID: n.ID,
			ID:             resourceID,
			PersonID:       n.PersonID,
			EventCreatedAt: event.CreatedAt,
			Properties:     eventProperties,
		}

		payloads = append(payloads, payload)
	}

	if n.TeamID != nil {
		teamSpec, err := teams.GetTeamSpec(ctx, n.TeamID.String())
		if err != nil {
			return nil, fmt.Errorf("failed to get team (id=%s); %v", n.TeamID, err)
		}

		for _, cn := range teamSpec.Notifications {
			if valid, err := expression.Eval(cn.Filter, celEnv, allEnvVars); err != nil {
				logs.IfError(db.UpdateNotificationError(n.ID.String(), err.Error()), "failed to update notification")
			} else if !valid {
				continue
			}

			payload := NotificationEventPayload{
				EventName:        event.Name,
				NotificationID:   n.ID,
				ID:               resourceID,
				TeamID:           n.TeamID.String(),
				NotificationName: cn.Name,
				EventCreatedAt:   event.CreatedAt,
				Properties:       eventProperties,
			}

			payloads = append(payloads, payload)
		}
	}

	for _, cn := range n.CustomNotifications {
		if valid, err := expression.Eval(cn.Filter, celEnv, allEnvVars); err != nil {
			logs.IfError(db.UpdateNotificationError(n.ID.String(), err.Error()), "failed to update notification")
		} else if !valid {
			continue
		}

		payload := NotificationEventPayload{
			EventName:      event.Name,
			NotificationID: n.ID,
			ID:             resourceID,
			EventCreatedAt: event.CreatedAt,
			Properties:     eventProperties,
		}

		payloads = append(payloads, payload)
	}

	return payloads, nil
}
