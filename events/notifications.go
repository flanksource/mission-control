package events

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/flanksource/commons/template"
	"github.com/flanksource/commons/utils"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	pkgNotification "github.com/flanksource/incident-commander/notification"
	pkgResponder "github.com/flanksource/incident-commander/responder"
	"github.com/flanksource/incident-commander/teams"
)

func NewNotificationUpdatesConsumerSync() SyncEventConsumer {
	return SyncEventConsumer{
		watchEvents: []string{EventNotificationUpdate, EventNotificationDelete},
		consumers: []SyncEventHandlerFunc{
			handleNotificationUpdates,
		},
	}
}

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

func sendNotifications(ctx *api.Context, events []api.Event) []api.Event {
	var failedEvents []api.Event
	for _, e := range events {
		if err := sendNotification(ctx, e); err != nil {
			e.Error = err.Error()
			failedEvents = append(failedEvents, e)
		}
	}

	return failedEvents
}

type NotificationEventProperties struct {
	ID               string    `json:"id"`                          // Resource id. depends what it is based on the original event.
	EventName        string    `json:"event_name"`                  // The name of the original event this notification is for.
	PersonID         string    `json:"person_id,omitempty"`         // The person recipient.
	TeamID           string    `json:"team_id,omitempty"`           // The team recipient.
	NotificationName string    `json:"notification_name,omitempty"` // Name of the notification of a team or a custom service of the notification.
	NotificationID   string    `json:"notification_id,omitempty"`   // ID of the notification.
	EventCreatedAt   time.Time `json:"event_created_at"`            // Timestamp at which the original event was created
}

// NotificationTemplate holds in data for notification
// that'll be used by struct templater.
type NotificationTemplate struct {
	Title      string            `template:"true"`
	Message    string            `template:"true"`
	Properties map[string]string `template:"true"`
}

func (t *NotificationEventProperties) AsMap() map[string]string {
	m := make(map[string]string)
	b, _ := json.Marshal(&t)
	_ = json.Unmarshal(b, &m)
	return m
}

func (t *NotificationEventProperties) FromMap(m map[string]string) {
	b, _ := json.Marshal(m)
	_ = json.Unmarshal(b, &t)
}

// labelsTemplate is a helper func to generate the template for displaying labels
func labelsTemplate(field string) string {
	return fmt.Sprintf("{{if %s}}### Labels: \n{{range $k, $v := %s}}**{{$k}}**: {{$v}} \n{{end}}{{end}}", field, field)
}

// defaultTitleAndBody returns the default title and body for notification
// based on the given event.
func defaultTitleAndBody(event string) (title string, body string) {
	switch event {
	case EventCheckPassed:
		title = "Check {{.check.name}} has passed"
		body = fmt.Sprintf("Canary: {{.canary.name}} \nAgent: {{.agent.name}} {{if .check_status.message}}\nMessage: {{.check_status.message}} {{end}}\n%s \n\n[Reference]({{.permalink}})", labelsTemplate(".check.labels"))

	case EventCheckFailed:
		title = "Check {{.check.name}} has failed"
		body = fmt.Sprintf("Canary: {{.canary.name}} \nAgent: {{.agent.name}} \nError: {{.check_status.error}} \n%s \n\n[Reference]({{.permalink}})", labelsTemplate(".check.labels"))

	case EventComponentStatusHealthy, EventComponentStatusUnhealthy, EventComponentStatusInfo, EventComponentStatusWarning, EventComponentStatusError:
		title = "Component {{.component.name}} status updated to {{.component.status}}"
		body = fmt.Sprintf("%s\n[Reference]({{.permalink}})", labelsTemplate(".component.labels"))

	case EventIncidentCommentAdded:
		title = "{{.author.name}} left a comment on {{.incident.incident_id}}: {{.incident.title}}"
		body = "{{.comment.comment}}\n\n[Reference]({{.permalink}})"

	case EventIncidentCreated:
		title = "{{.incident.incident_id}}: {{.incident.title}} ({{.incident.severity}}) created"
		body = "Type: {{.incident.type}}\n\n[Reference]({{.permalink}})"

	case EventIncidentDODAdded:
		title = "Definition of Done added | {{.incident.incident_id}}: {{.incident.title}}"
		body = "Evidence: {{.evidence.description}}\n\n[Reference]({{.permalink}})"

	case EventIncidentDODPassed, EventIncidentDODRegressed:
		title = "Definition of Done {{if .evidence.done}}passed{{else}}regressed{{end}} | {{.incident.incident_id}}: {{.incident.title}}"
		body = "Evidence: {{.evidence.description}}\nHypothesis: {{.hypothesis.title}}\n\n[Reference]({{.permalink}})"

	case EventIncidentResponderAdded:
		title = "New responder added to {{.incident.incident_id}}: {{.incident.title}}"
		body = "Responder {{.responder.name}}\n\n[Reference]({{.permalink}})"

	case EventIncidentResponderRemoved:
		title = "Responder removed from {{.incident.incident_id}}: {{.incident.title}}"
		body = "Responder {{.responder.name}}\n\n[Reference]({{.permalink}})"

	case EventIncidentStatusCancelled, EventIncidentStatusClosed, EventIncidentStatusInvestigating, EventIncidentStatusMitigated, EventIncidentStatusOpen, EventIncidentStatusResolved:
		title = "{{.incident.title}} status updated"
		body = "New Status: {{.incident.status}}\n\n[Reference]({{.permalink}})"

	case EventTeamUpdate, EventTeamDelete, EventNotificationUpdate, EventNotificationDelete, EventPlaybookSpecApprovalUpdated, EventPlaybookApprovalInserted:
		// Not applicable
	}

	return title, body
}

func sendNotification(ctx *api.Context, event api.Event) error {
	var props NotificationEventProperties
	props.FromMap(event.Properties)

	originalEvent := api.Event{Name: props.EventName, CreatedAt: props.EventCreatedAt}
	celEnv, err := getEnvForEvent(ctx, originalEvent, event.Properties)
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

	notification, err := pkgNotification.GetNotification(ctx, props.NotificationID)
	if err != nil {
		return err
	}

	defaultTitle, defaultBody := defaultTitleAndBody(props.EventName)

	data := NotificationTemplate{
		Title:      utils.Coalesce(notification.Title, defaultTitle),
		Message:    utils.Coalesce(notification.Template, defaultBody),
		Properties: notification.Properties,
	}

	if err := templater.Walk(&data); err != nil {
		return fmt.Errorf("error templating notification: %w", err)
	}

	if props.PersonID != "" {
		var emailAddress string
		if err := ctx.DB().Model(&models.Person{}).Select("email").Where("id = ?", props.PersonID).Find(&emailAddress).Error; err != nil {
			return fmt.Errorf("failed to get email of person(id=%s); %v", props.PersonID, err)
		}

		smtpURL := fmt.Sprintf("%s?ToAddresses=%s", pkgNotification.SystemSMTP, url.QueryEscape(emailAddress))
		return pkgNotification.Send(ctx, "", smtpURL, data.Title, data.Message, data.Properties)
	}

	if props.TeamID != "" {
		teamSpec, err := teams.GetTeamSpec(ctx, props.TeamID)
		if err != nil {
			return fmt.Errorf("failed to get team(id=%s); %v", props.TeamID, err)
		}

		for _, cn := range teamSpec.Notifications {
			if cn.Name != props.NotificationName {
				continue
			}

			if err := templater.Walk(&cn); err != nil {
				return fmt.Errorf("error templating notification: %w", err)
			}

			return pkgNotification.Send(ctx, cn.Connection, cn.URL, data.Title, data.Message, data.Properties, cn.Properties)
		}
	}

	for _, cn := range notification.CustomNotifications {
		if cn.Name != props.NotificationName {
			continue
		}

		if err := templater.Walk(&cn); err != nil {
			return fmt.Errorf("error templating notification: %w", err)
		}

		return pkgNotification.Send(ctx, cn.Connection, cn.URL, data.Title, data.Message, data.Properties, cn.Properties)
	}

	return nil
}

// addNotificationEvent responds to a event that can possible generate a notification.
// If a notification is found for the given event and passes all the filters, then
// a new notification event is created.
func addNotificationEvent(ctx *api.Context, event api.Event) error {
	notificationIDs, err := pkgNotification.GetNotificationIDs(ctx, event.Name)
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
		n, err := pkgNotification.GetNotification(ctx, id)
		if err != nil {
			return err
		}

		if !n.HasRecipients() {
			continue
		}

		expressionRunner := pkgNotification.ExpressionRunner{
			ResourceID:   id,
			ResourceType: "notification",
			CelEnv:       celEnv,
		}

		if valid, err := expressionRunner.Eval(ctx, n.Filter); err != nil || !valid {
			// We consider an error in filter evaluation is a failed filter check.
			// Mostly, the filter check returns an error if the variable isn't defined.
			// Example: If the filter makes use of `check` variable but the event is for
			// incident creation, then the expression evaluation returns an error.
			continue
		}

		if n.PersonID != nil {
			prop := NotificationEventProperties{
				EventName:      event.Name,
				NotificationID: n.ID.String(),
				ID:             event.Properties["id"],
				PersonID:       n.PersonID.String(),
				EventCreatedAt: event.CreatedAt,
			}

			newEvent := &api.Event{
				Name:       EventNotificationSend,
				Properties: prop.AsMap(),
			}
			if err := ctx.DB().Create(newEvent).Error; err != nil {
				return fmt.Errorf("failed to create notification event for person(id=%s): %v", n.PersonID, err)
			}
		}

		if n.TeamID != nil {
			teamSpec, err := teams.GetTeamSpec(ctx, n.TeamID.String())
			if err != nil {
				return fmt.Errorf("failed to get team(id=%s); %v", n.TeamID, err)
			}

			expressionRunner := pkgNotification.ExpressionRunner{
				ResourceID:   id,
				ResourceType: "notification",
				CelEnv:       celEnv,
			}

			for _, cn := range teamSpec.Notifications {
				if valid, err := expressionRunner.Eval(ctx, cn.Filter); err != nil || !valid {
					continue
				}

				prop := NotificationEventProperties{
					EventName:        event.Name,
					NotificationID:   n.ID.String(),
					ID:               event.Properties["id"],
					TeamID:           n.TeamID.String(),
					NotificationName: cn.Name,
				}

				newEvent := &api.Event{
					Name:       EventNotificationSend,
					Properties: prop.AsMap(),
				}

				if err := ctx.DB().Create(newEvent).Error; err != nil {
					return fmt.Errorf("failed to create notification event for team(id=%s): %v", n.TeamID, err)
				}
			}
		}

		for _, cn := range n.CustomNotifications {
			if valid, err := expressionRunner.Eval(ctx, cn.Filter); err != nil || !valid {
				continue
			}

			prop := NotificationEventProperties{
				EventName:        event.Name,
				NotificationID:   n.ID.String(),
				ID:               event.Properties["id"],
				NotificationName: cn.Name,
			}

			newEvent := &api.Event{
				Name:       EventNotificationSend,
				Properties: prop.AsMap(),
			}

			if err := ctx.DB().Create(newEvent).Error; err != nil {
				return fmt.Errorf("failed to create notification event for custom service (id=%s): %v", n.PersonID, err)
			}
		}
	}

	return nil
}

// getEnvForEvent gets the environment variables for the given event
// that'll be passed to the cel expression or to the template renderer as a view.
func getEnvForEvent(ctx *api.Context, event api.Event, properties map[string]string) (map[string]any, error) {
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
		} else if agent == nil {
			return nil, fmt.Errorf("agent(id=%s) not found", check.AgentID)
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
		if err := ctx.DB().Where("check_id = ?", checkID).Where("status = ?", check.Status == models.CheckStatusHealthy).Where("created_at >= ?", event.CreatedAt).Order("created_at").First(&checkStatus).Error; err != nil {
			return nil, fmt.Errorf("failed to get check status: %w", err)
		}

		env["check_status"] = checkStatus.AsMap()
		env["canary"] = canary.AsMap("spec")
		env["check"] = check.AsMap("spec")
		env["agent"] = agent.AsMap()
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

		author, err := duty.FindCachedPerson(ctx, comment.CreatedBy.String())
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

		var incident models.Incident
		if err := ctx.DB().Where("id = ?", hypotheses.IncidentID).Find(&incident).Error; err != nil {
			return nil, err
		}

		env["evidence"] = evidence.AsMap()
		env["hypotheses"] = hypotheses.AsMap()
		env["incident"] = incident.AsMap()
		env["permalink"] = fmt.Sprintf("%s/incidents/%s", api.PublicWebURL, incident.ID)
	}

	return env, nil
}

func handleNotificationUpdates(ctx *api.Context, event api.Event) error {
	if id, ok := event.Properties["id"]; ok {
		pkgNotification.PurgeCache(id)
	}

	return nil
}
