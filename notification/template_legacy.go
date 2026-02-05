package notification

import (
	"embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/flanksource/commons/utils"
	"github.com/flanksource/duty/context"

	"github.com/flanksource/incident-commander/api"
)

//go:embed templates/*
var templates embed.FS

const groupedResourcesMessage = `
Resources grouped with this notification:
{{- range .groupedResources }}
- {{ . }}
{{- end }}`

// DefaultTitleAndBody returns the default title and body for notification
// based on the given event.
func DefaultTitleAndBody(event string) (title string, body string) {
	switch event {
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
