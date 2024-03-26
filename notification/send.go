package notification

import (
	"encoding/json"
	"fmt"
	"net/url"
	"time"

	"github.com/flanksource/commons/utils"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/postq"
	"github.com/google/uuid"

	"github.com/flanksource/incident-commander/api"

	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/logs"
	"github.com/flanksource/incident-commander/teams"
	"github.com/flanksource/incident-commander/utils/expression"
)

// List of all possible variables for any expression related to notifications
var allEnvVars = []string{"check", "canary", "component", "incident", "team", "responder", "comment", "evidence", "hypothesis"}

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

// SendNotification generates the notification from the given event and sends it.
func SendNotification(ctx *Context, payload NotificationEventPayload, celEnv map[string]any) error {
	templater := ctx.NewStructTemplater(celEnv, "", nil)

	notification, err := GetNotification(ctx.Context, payload.NotificationID.String())
	if err != nil {
		return err
	}

	defaultTitle, defaultBody := defaultTitleAndBody(payload.EventName)

	data := NotificationTemplate{
		Title:      utils.Coalesce(notification.Title, defaultTitle),
		Message:    utils.Coalesce(notification.Template, defaultBody),
		Properties: notification.Properties,
	}

	if err := templater.Walk(&data); err != nil {
		return fmt.Errorf("error templating notification: %w", err)
	}

	if payload.PersonID != nil {
		ctx.WithPersonID(payload.PersonID).WithRecipientType(RecipientTypePerson)
		var emailAddress string
		if err := ctx.DB().Model(&models.Person{}).Select("email").Where("id = ?", payload.PersonID).Find(&emailAddress).Error; err != nil {
			return fmt.Errorf("failed to get email of person(id=%s); %v", payload.PersonID, err)
		}

		smtpURL := fmt.Sprintf("%s?ToAddresses=%s", api.SystemSMTP, url.QueryEscape(emailAddress))
		return Send(ctx, "", smtpURL, data.Title, data.Message, data.Properties)
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

			if err := templater.Walk(&cn); err != nil {
				return fmt.Errorf("error templating notification: %w", err)
			}

			return Send(ctx, cn.Connection, cn.URL, data.Title, data.Message, data.Properties, cn.Properties)
		}
	}

	for _, cn := range notification.CustomNotifications {
		ctx.WithRecipientType(RecipientTypeCustom)

		if err := templater.Walk(&cn); err != nil {
			return fmt.Errorf("error templating notification: %w", err)
		}

		return Send(ctx, cn.Connection, cn.URL, data.Title, data.Message, data.Properties, cn.Properties)
	}

	return nil
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
		body = fmt.Sprintf(`Canary: {{.canary.name}}
{{if .agent}}Agent: {{.agent.name}}{{end}}
{{if .status.message}}Message: {{.status.message}} {{end}}
%s

[Reference]({{.permalink}})`, labelsTemplate(".check.labels"))

	case api.EventCheckFailed:
		title = "Check {{.check.name}} has failed"
		body = fmt.Sprintf(`Canary: {{.canary.name}}
{{if .agent}}Agent: {{.agent.name}}{{end}}
Error: {{.status.error}}
%s

[Reference]({{.permalink}})`, labelsTemplate(".check.labels"))

	case api.EventComponentStatusHealthy, api.EventComponentStatusUnhealthy, api.EventComponentStatusInfo, api.EventComponentStatusWarning, api.EventComponentStatusError:
		title = "Component {{.component.name}} status updated to {{.component.status}}"
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

func CreateNotificationSendPayloads(ctx context.Context, event postq.Event, n *NotificationWithSpec, celEnv map[string]any) ([]NotificationEventPayload, error) {
	var payloads []NotificationEventPayload

	resourceID, err := uuid.Parse(event.Properties["id"])
	if err != nil {
		return nil, fmt.Errorf("failed to parse resource id: %v", err)
	}

	if n.PersonID != nil {
		payload := NotificationEventPayload{
			EventName:      event.Name,
			NotificationID: n.ID,
			ID:             resourceID,
			PersonID:       n.PersonID,
			EventCreatedAt: event.CreatedAt,
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
		}

		payloads = append(payloads, payload)
	}

	return payloads, nil
}
