package events

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/flanksource/commons/template"
	cUtils "github.com/flanksource/commons/utils"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/logs"
	pkgNotification "github.com/flanksource/incident-commander/notification"
	pkgResponder "github.com/flanksource/incident-commander/responder"
	"github.com/flanksource/incident-commander/teams"
	"github.com/google/uuid"
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

func sendNotifications(ctx api.Context, events []api.Event) []api.Event {
	var failedEvents []api.Event
	for _, e := range events {
		var props NotificationEventPayload
		props.FromMap(e.Properties)

		notificationContext := pkgNotification.NewContext(ctx, props.NotificationID)
		notificationContext.WithSource(props.EventName, props.ID)
		logs.IfError(notificationContext.StartLog(), "error persisting start of notification send history")

		if err := sendNotification(notificationContext, e); err != nil {
			e.Error = err.Error()
			failedEvents = append(failedEvents, e)
			notificationContext.WithError(err.Error())
		}

		logs.IfError(notificationContext.EndLog(), "error persisting end of notification send history")
	}

	return failedEvents
}

// NotificationEventPayload holds data to create a notification
type NotificationEventPayload struct {
	ID               uuid.UUID  `json:"id"`                          // Resource id. depends what it is based on the original event.
	EventName        string     `json:"event_name"`                  // The name of the original event this notification is for.
	PersonID         *uuid.UUID `json:"person_id,omitempty"`         // The person recipient.
	TeamID           string     `json:"team_id,omitempty"`           // The team recipient.
	NotificationName string     `json:"notification_name,omitempty"` // Name of the notification of a team or a custom service of the notification.
	NotificationID   uuid.UUID  `json:"notification_id,omitempty"`   // ID of the notification.
	EventCreatedAt   time.Time  `json:"event_created_at"`            // Timestamp at which the original event was created
}

// NotificationTemplate holds in data for notification
// that'll be used by struct templater.
type NotificationTemplate struct {
	Title      string            `template:"true"`
	Message    string            `template:"true"`
	Properties map[string]string `template:"true"`
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
		body = fmt.Sprintf(`Canary: {{.canary.name}}
{{if .agent}}Agent: {{.agent.name}}{{end}}
{{if .status.message}}Message: {{.status.message}} {{end}}
%s

[Reference]({{.permalink}})`, labelsTemplate(".check.labels"))

	case EventCheckFailed:
		title = "Check {{.check.name}} has failed"
		body = fmt.Sprintf(`Canary: {{.canary.name}}
{{if .agent}}Agent: {{.agent.name}}{{end}}
Error: {{.status.error}}
%s

[Reference]({{.permalink}})`, labelsTemplate(".check.labels"))

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
		body = `Evidence: {{.evidence.description}}
Hypothesis: {{.hypothesis.title}}

[Reference]({{.permalink}})`

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

func sendNotification(ctx *pkgNotification.Context, event api.Event) error {
	var props NotificationEventPayload
	props.FromMap(event.Properties)

	originalEvent := api.Event{Name: props.EventName, CreatedAt: props.EventCreatedAt}
	celEnv, err := getEnvForEvent(ctx.Context, originalEvent, event.Properties)
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

	notification, err := pkgNotification.GetNotification(ctx.Context, props.NotificationID.String())
	if err != nil {
		return err
	}

	defaultTitle, defaultBody := defaultTitleAndBody(props.EventName)

	data := NotificationTemplate{
		Title:      cUtils.Coalesce(notification.Title, defaultTitle),
		Message:    cUtils.Coalesce(notification.Template, defaultBody),
		Properties: notification.Properties,
	}

	if err := templater.Walk(&data); err != nil {
		return fmt.Errorf("error templating notification: %w", err)
	}

	if props.PersonID != nil {
		ctx.WithPersonID(props.PersonID)
		var emailAddress string
		if err := ctx.DB().Model(&models.Person{}).Select("email").Where("id = ?", props.PersonID).Find(&emailAddress).Error; err != nil {
			return fmt.Errorf("failed to get email of person(id=%s); %v", props.PersonID, err)
		}

		smtpURL := fmt.Sprintf("%s?ToAddresses=%s", api.SystemSMTP, url.QueryEscape(emailAddress))
		return pkgNotification.Send(ctx, "", smtpURL, data.Title, data.Message, data.Properties)
	}

	if props.TeamID != "" {
		teamSpec, err := teams.GetTeamSpec(ctx.Context, props.TeamID)
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
func addNotificationEvent(ctx api.Context, event api.Event) error {
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
			resourceID, err := uuid.Parse(event.Properties["id"])
			if err != nil {
				return fmt.Errorf("failed to parse resource id: %v", err)
			}

			prop := NotificationEventPayload{
				EventName:      event.Name,
				NotificationID: n.ID,
				ID:             resourceID,
				PersonID:       n.PersonID,
				EventCreatedAt: event.CreatedAt,
			}

			newEvent := &api.Event{
				Name:       EventNotificationSend,
				Properties: prop.AsMap(),
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

			expressionRunner := pkgNotification.ExpressionRunner{
				ResourceID:   id,
				ResourceType: "notification",
				CelEnv:       celEnv,
			}

			for _, cn := range teamSpec.Notifications {
				if valid, err := expressionRunner.Eval(ctx, cn.Filter); err != nil || !valid {
					continue
				}

				resourceID, err := uuid.Parse(event.Properties["id"])
				if err != nil {
					return fmt.Errorf("failed to parse resource id: %v", err)
				}

				prop := NotificationEventPayload{
					EventName:        event.Name,
					NotificationID:   n.ID,
					ID:               resourceID,
					TeamID:           n.TeamID.String(),
					NotificationName: cn.Name,
				}

				newEvent := &api.Event{
					Name:       EventNotificationSend,
					Properties: prop.AsMap(),
				}

				if err := ctx.DB().Clauses(eventQueueOnConflictClause).Create(newEvent).Error; err != nil {
					return fmt.Errorf("failed to create notification event for team(id=%s): %v", n.TeamID, err)
				}
			}
		}

		for _, cn := range n.CustomNotifications {
			if valid, err := expressionRunner.Eval(ctx, cn.Filter); err != nil || !valid {
				continue
			}

			resourceID, err := uuid.Parse(event.Properties["id"])
			if err != nil {
				return fmt.Errorf("failed to parse resource id: %v", err)
			}

			prop := NotificationEventPayload{
				EventName:        event.Name,
				NotificationID:   n.ID,
				ID:               resourceID,
				NotificationName: cn.Name,
			}

			newEvent := &api.Event{
				Name:       EventNotificationSend,
				Properties: prop.AsMap(),
			}

			if err := ctx.DB().Clauses(eventQueueOnConflictClause).Create(newEvent).Error; err != nil {
				return fmt.Errorf("failed to create notification event for custom service (name:%s): %v", cn.Name, err)
			}
		}
	}

	return nil
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

func handleNotificationUpdates(ctx api.Context, event api.Event) error {
	if id, ok := event.Properties["id"]; ok {
		pkgNotification.PurgeCache(id)
	}

	return nil
}
