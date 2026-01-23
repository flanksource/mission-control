package notification

import (
	"fmt"
	"sort"
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/samber/lo"

	icapi "github.com/flanksource/incident-commander/api"
)

// NotificationField represents a labeled value shown in a details table.
type NotificationField struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// NotificationAction represents a call-to-action rendered as a button/link.
type NotificationAction struct {
	Label string `json:"label"`
	URL   string `json:"url"`
	Style string `json:"style,omitempty"`
}

// NotificationMessagePayload is the channel-agnostic payload stored in history
// and formatted via clicky for each delivery channel.
type NotificationMessagePayload struct {
	EventName             string               `json:"event_name,omitempty"`
	Title                 string               `json:"title,omitempty"`
	SlackTitle            string               `json:"slack_title,omitempty"`
	Summary               string               `json:"summary,omitempty"`
	Description           string               `json:"description,omitempty"`
	Fields                []NotificationField  `json:"fields,omitempty"`
	Labels                map[string]string    `json:"labels,omitempty"`
	LabelFields           []NotificationField  `json:"label_fields,omitempty"`
	RecentEvents          []string             `json:"recent_events,omitempty"`
	GroupedResources      []string             `json:"grouped_resources,omitempty"`
	GroupedResourcesTitle string               `json:"grouped_resources_title,omitempty"`
	Actions               []NotificationAction `json:"actions,omitempty"`
	SlackActionsDivider   bool                 `json:"slack_actions_divider,omitempty"`
}

// BuildNotificationMessagePayload builds a channel-agnostic payload for the given event.
func BuildNotificationMessagePayload(payload NotificationEventPayload, env *celVariables) NotificationMessagePayload {
	msg := NotificationMessagePayload{EventName: payload.EventName}
	if env == nil {
		msg.Title = payload.EventName
		return msg
	}

	switch payload.EventName {
	case icapi.EventCheckFailed:
		msg.Title = fmt.Sprintf("Check %s has failed", safeName(lo.FromPtr(env.Check).Name))
		msg.SlackTitle = fmt.Sprintf(":red_circle: *%s* is _unhealthy_", safeName(lo.FromPtr(env.Check).Name))
		msg.Description = lo.FromPtr(env.CheckStatus).Error
		msg.Fields = append(msg.Fields,
			field("Canary", safeName(lo.FromPtr(env.Canary).Name)),
			field("Namespace", safeName(lo.FromPtr(env.Canary).Namespace)),
		)
		msg.Fields = addAgentField(msg.Fields, env)
		msg.Labels = mapLabelsFromJSONStringMap(lo.FromPtr(env.Check).Labels)
		msg.LabelFields = labelFieldsFromLabels(lo.FromPtr(env.Check).GetTrimmedLabels())
		msg.GroupedResources = env.GroupedResources
		msg.GroupedResourcesTitle = "Resources grouped with notification"
		msg.Actions = []NotificationAction{
			{Label: "View Health Check", URL: env.Permalink},
			{Label: "🔕 Silence", URL: env.SilenceURL},
		}
	case icapi.EventCheckPassed:
		msg.Title = fmt.Sprintf("Check %s has passed", safeName(lo.FromPtr(env.Check).Name))
		msg.SlackTitle = fmt.Sprintf(":large_green_circle: *%s* is _healthy_", safeName(lo.FromPtr(env.Canary).Name))
		msg.Description = lo.FromPtr(env.CheckStatus).Message
		msg.Fields = append(msg.Fields,
			field("Canary", safeName(lo.FromPtr(env.Canary).Name)),
			field("Namespace", safeName(lo.FromPtr(env.Canary).Namespace)),
		)
		msg.Fields = addAgentField(msg.Fields, env)
		msg.Labels = mapLabelsFromJSONStringMap(lo.FromPtr(env.Check).Labels)
		msg.LabelFields = labelFieldsFromLabels(lo.FromPtr(env.Check).GetTrimmedLabels())
		msg.GroupedResources = env.GroupedResources
		msg.GroupedResourcesTitle = "Resources grouped with notification"
		msg.Actions = []NotificationAction{
			{Label: "View Health Check", URL: env.Permalink},
			{Label: "🔕 Silence", URL: env.SilenceURL},
		}
	case icapi.EventConfigHealthy, icapi.EventConfigUnhealthy, icapi.EventConfigWarning, icapi.EventConfigUnknown, icapi.EventConfigDegraded:
		configHealth := healthValue(lo.FromPtr(env.ConfigItem).Health)
		msg.Title = fmt.Sprintf("%s %s is %s", safeName(stringPtr(lo.FromPtr(env.ConfigItem).Type)), safeName(stringPtr(lo.FromPtr(env.ConfigItem).Name)), configHealth)
		msg.SlackTitle = fmt.Sprintf("%s *%s* is _%s_", slackEmojiStatus(configHealth), safeName(stringPtr(lo.FromPtr(env.ConfigItem).Name)), configHealth)
		msg.Description = coalesceString(stringPtr(lo.FromPtr(env.ConfigItem).Description), payload.ResourceHealthDescription)
		msg.Fields = append(msg.Fields,
			field("Type", stringPtr(lo.FromPtr(env.ConfigItem).Type)),
			field("Status", stringPtr(lo.FromPtr(env.ConfigItem).Status)),
		)
		msg.Fields = addAgentField(msg.Fields, env)
		msg.Labels = mapLabelsFromJSONStringMapPtr(lo.FromPtr(env.ConfigItem).Labels)
		msg.LabelFields = labelFieldsFromLabels(lo.FromPtr(env.ConfigItem).GetTrimmedLabels())
		msg.RecentEvents = env.RecentEvents
		msg.GroupedResources = env.GroupedResources
		msg.GroupedResourcesTitle = "Also Failing"
		msg.SlackActionsDivider = true
		msg.Actions = []NotificationAction{
			{Label: "View Catalog", URL: env.Permalink},
			{Label: "🔕 Silence", URL: env.SilenceURL},
		}
	case icapi.EventConfigCreated, icapi.EventConfigUpdated, icapi.EventConfigDeleted, icapi.EventConfigChanged:
		msg.Title = fmt.Sprintf("%s %s was %s", safeName(stringPtr(lo.FromPtr(env.ConfigItem).Type)), safeName(stringPtr(lo.FromPtr(env.ConfigItem).Name)), env.NewState)
		msg.SlackTitle = fmt.Sprintf(":information_source: *%s* was _%s_", safeName(stringPtr(lo.FromPtr(env.ConfigItem).Name)), env.NewState)
		msg.Description = coalesceString(stringPtr(lo.FromPtr(env.ConfigItem).Description), payload.ResourceHealthDescription)
		msg.Fields = append(msg.Fields,
			field("Type", stringPtr(lo.FromPtr(env.ConfigItem).Type)),
			field("Status", stringPtr(lo.FromPtr(env.ConfigItem).Status)),
		)
		msg.Fields = addAgentField(msg.Fields, env)
		msg.Labels = mapLabelsFromJSONStringMapPtr(lo.FromPtr(env.ConfigItem).Labels)
		msg.LabelFields = labelFieldsFromLabels(lo.FromPtr(env.ConfigItem).GetTrimmedLabels())
		msg.GroupedResources = env.GroupedResources
		msg.GroupedResourcesTitle = "Also Failing"
		msg.Actions = []NotificationAction{
			{Label: "View Catalog", URL: env.Permalink},
			{Label: "🔕 Silence", URL: env.SilenceURL},
		}
	case icapi.EventComponentHealthy, icapi.EventComponentUnhealthy, icapi.EventComponentWarning, icapi.EventComponentUnknown:
		componentHealth := healthValue(lo.FromPtr(env.Component).Health)
		msg.Title = fmt.Sprintf("Component %s is %s", safeName(lo.FromPtr(env.Component).Name), componentHealth)
		msg.SlackTitle = fmt.Sprintf("%s *%s* is _%s_", slackEmojiStatus(componentHealth), safeName(lo.FromPtr(env.Component).Name), componentHealth)
		msg.Description = coalesceString(lo.FromPtr(env.Component).Description, payload.ResourceHealthDescription)
		msg.Fields = append(msg.Fields,
			field("Type", lo.FromPtr(env.Component).Type),
			field("Status", stringValue(lo.FromPtr(env.Component).Status)),
		)
		msg.Fields = addAgentField(msg.Fields, env)
		msg.Labels = mapLabelsFromJSONStringMap(lo.FromPtr(env.Component).Labels)
		msg.LabelFields = labelFieldsFromLabels(lo.FromPtr(env.Component).GetTrimmedLabels())
		msg.GroupedResources = env.GroupedResources
		msg.GroupedResourcesTitle = "Also Failing"
		msg.Actions = []NotificationAction{
			{Label: "View Component", URL: env.Permalink},
			{Label: "🔕 Silence", URL: env.SilenceURL},
		}
	case icapi.EventIncidentCommentAdded:
		msg.Title = fmt.Sprintf("%s left a comment on %s: %s", safeName(lo.FromPtr(env.Author).Name), lo.FromPtr(env.Incident).IncidentID, lo.FromPtr(env.Incident).Title)
		msg.Description = lo.FromPtr(env.Comment).Comment
		msg.Actions = []NotificationAction{{Label: "Reference", URL: env.Permalink}}
	case icapi.EventIncidentCreated:
		msg.Title = fmt.Sprintf("%s: %s (%s) created", lo.FromPtr(env.Incident).IncidentID, lo.FromPtr(env.Incident).Title, lo.FromPtr(env.Incident).Severity)
		msg.Fields = append(msg.Fields,
			field("Type", string(lo.FromPtr(env.Incident).Type)),
			field("Severity", stringValue(lo.FromPtr(env.Incident).Severity)),
		)
		msg.Actions = []NotificationAction{{Label: "Reference", URL: env.Permalink}}
	case icapi.EventIncidentDODAdded:
		msg.Title = fmt.Sprintf("Definition of Done added | %s: %s", lo.FromPtr(env.Incident).IncidentID, lo.FromPtr(env.Incident).Title)
		msg.Description = lo.FromPtr(env.Evidence).Description
		msg.Actions = []NotificationAction{{Label: "Reference", URL: env.Permalink}}
	case icapi.EventIncidentDODPassed, icapi.EventIncidentDODRegressed:
		msg.Title = fmt.Sprintf("Definition of Done %s | %s: %s", dodStatus(payload.EventName), lo.FromPtr(env.Incident).IncidentID, lo.FromPtr(env.Incident).Title)
		msg.Description = lo.FromPtr(env.Evidence).Description
		msg.Fields = append(msg.Fields, field("Hypothesis", lo.FromPtr(env.Hypothesis).Title))
		msg.Actions = []NotificationAction{{Label: "Reference", URL: env.Permalink}}
	case icapi.EventIncidentResponderAdded:
		msg.Title = fmt.Sprintf("New responder added to %s: %s", lo.FromPtr(env.Incident).IncidentID, lo.FromPtr(env.Incident).Title)
		msg.Description = fmt.Sprintf("Responder %s", lo.FromPtr(env.Responder).ID)
		msg.Actions = []NotificationAction{{Label: "Reference", URL: env.Permalink}}
	case icapi.EventIncidentResponderRemoved:
		msg.Title = fmt.Sprintf("Responder removed from %s: %s", lo.FromPtr(env.Incident).IncidentID, lo.FromPtr(env.Incident).Title)
		msg.Description = fmt.Sprintf("Responder %s", lo.FromPtr(env.Responder).ID)
		msg.Actions = []NotificationAction{{Label: "Reference", URL: env.Permalink}}
	case icapi.EventIncidentStatusCancelled, icapi.EventIncidentStatusClosed, icapi.EventIncidentStatusInvestigating, icapi.EventIncidentStatusMitigated, icapi.EventIncidentStatusOpen, icapi.EventIncidentStatusResolved:
		msg.Title = fmt.Sprintf("%s status updated", lo.FromPtr(env.Incident).Title)
		msg.Description = fmt.Sprintf("New Status: %s", lo.FromPtr(env.Incident).Status)
		msg.Actions = []NotificationAction{{Label: "Reference", URL: env.Permalink}}
	default:
		msg.Title = payload.EventName
	}

	msg.Fields = compactFields(msg.Fields)
	msg.LabelFields = compactFields(msg.LabelFields)
	return msg
}

// ToTextList converts the payload into clicky primitives for formatting.
func (p NotificationMessagePayload) ToTextList() api.TextList {
	return p.toTextList(true)
}

// ToSlackTextList converts the payload into clicky primitives optimized for Slack.
func (p NotificationMessagePayload) ToSlackTextList() api.TextList {
	return p.toSlackTextList()
}

func (p NotificationMessagePayload) toTextList(includeLabelHeading bool) api.TextList {
	var out api.TextList

	if p.Title != "" {
		out = append(out, api.Text{Content: p.Title, Style: "header text-xl font-semibold"})
	}

	contentItems := 0
	addDivider := func() {
		if len(out) > 0 {
			out = append(out, api.HR)
		}
	}

	if p.Summary != "" {
		contentItems++
	}
	if p.Description != "" {
		contentItems++
	}
	if len(p.Fields) > 0 {
		contentItems++
	}
	if len(p.Labels) > 0 || len(p.RecentEvents) > 0 || len(p.GroupedResources) > 0 {
		contentItems++
	}
	if contentItems > 0 {
		addDivider()
	}

	if p.Summary != "" {
		out = append(out, api.Text{Content: p.Summary})
	}

	if p.Description != "" {
		out = append(out, api.Text{Content: p.Description})
	}

	if len(p.Fields) > 0 {
		out = append(out, fieldsTable(p.Fields))
	}

	labelFields := p.LabelFields
	if len(labelFields) == 0 && len(p.Labels) > 0 {
		labelFields = labelFieldsFromMap(p.Labels)
	}
	if len(labelFields) > 0 {
		if includeLabelHeading {
			out = append(out, api.Text{Content: "Labels", Style: "font-semibold"})
		}
		out = append(out, fieldsTable(labelFields))
	}

	if len(p.RecentEvents) > 0 {
		out = append(out, labeledInlineList("Recent Events", p.RecentEvents))
	}

	if len(p.GroupedResources) > 0 {
		title := p.GroupedResourcesTitle
		if title == "" {
			title = "Grouped Resources"
		}
		out = append(out, labeledList(title, p.GroupedResources))
	}

	if len(p.Actions) > 0 {
		addDivider()
		out = append(out, actionsToButtonGroup(p.Actions))
	}

	return out
}

func (p NotificationMessagePayload) toSlackTextList() api.TextList {
	var out api.TextList

	title := p.Title
	if p.SlackTitle != "" {
		title = p.SlackTitle
	}
	if title != "" {
		out = append(out, api.Text{Content: title, Style: "slack-section"})
	}

	contentItems := 0
	addDivider := func() {
		if len(out) > 0 {
			out = append(out, api.HR)
		}
	}

	if p.Summary != "" {
		contentItems++
	}
	if p.Description != "" {
		contentItems++
	}
	if len(p.Fields) > 0 {
		contentItems++
	}
	if len(p.Labels) > 0 || len(p.LabelFields) > 0 || len(p.RecentEvents) > 0 || len(p.GroupedResources) > 0 {
		contentItems++
	}
	if contentItems > 0 {
		addDivider()
	}

	if p.Summary != "" {
		out = append(out, api.Text{Content: p.Summary, Style: "slack-section"})
	}

	if p.Description != "" {
		out = append(out, api.Text{Content: p.Description, Style: slackDescriptionStyle(p.EventName)})
	}

	if len(p.Fields) > 0 {
		out = append(out, fieldsTable(p.Fields))
	}

	labelFields := p.LabelFields
	if len(labelFields) == 0 && len(p.Labels) > 0 {
		labelFields = labelFieldsFromMap(p.Labels)
	}
	if len(labelFields) > maxSlackFieldsPerSection {
		labelFields = labelFields[:maxSlackFieldsPerSection]
	}
	if len(labelFields) > 0 {
		out = append(out, fieldsTable(labelFields))
	}

	if len(p.RecentEvents) > 0 {
		out = append(out, api.Text{Content: slackRecentEventsText(p.RecentEvents), Style: "slack-section"})
	}

	if len(p.GroupedResources) > 0 {
		out = append(out, api.Text{Content: slackGroupedResourcesText(p), Style: "slack-section"})
	}

	if len(p.Actions) > 0 {
		if p.SlackActionsDivider {
			addDivider()
		}
		out = append(out, actionsToButtonGroup(p.Actions))
	}

	return out
}

// FormatNotificationMessage renders a payload using clicky for the given format.
func FormatNotificationMessage(payload NotificationMessagePayload, format string) (string, error) {
	if strings.EqualFold(format, "slack") {
		return clicky.Format(payload.ToSlackTextList(), clicky.FormatOptions{Format: format})
	}
	return clicky.Format(payload.ToTextList(), clicky.FormatOptions{Format: format})
}

func field(label, value string) NotificationField {
	return NotificationField{Label: label, Value: strings.TrimSpace(value)}
}

func compactFields(fields []NotificationField) []NotificationField {
	out := make([]NotificationField, 0, len(fields))
	for _, f := range fields {
		if f.Value == "" || f.Label == "" {
			continue
		}
		out = append(out, f)
	}
	return out
}

func addAgentField(fields []NotificationField, env *celVariables) []NotificationField {
	if env.Agent == nil || env.Agent.Name == "" || env.Agent.Name == "local" {
		return fields
	}
	return append(fields, field("Agent", env.Agent.Name))
}

func fieldsTable(fields []NotificationField) api.TextTable {
	headers := make(api.TextList, 0, len(fields))
	fieldNames := make([]string, 0, len(fields))
	row := api.TableRow{}

	for _, f := range fields {
		key := f.Label
		headers = append(headers, api.Text{Content: f.Label})
		fieldNames = append(fieldNames, key)
		row[key] = api.NewTypedValue(f.Value)
	}

	return api.TextTable{
		Headers:    headers,
		FieldNames: fieldNames,
		Rows:       []api.TableRow{row},
	}
}

func labeledList(label string, items []string) api.Text {
	title := api.Text{Content: label + ": ", Style: "font-semibold"}
	return title.Add(api.Text{Content: strings.Join(items, "\n")})
}

func labeledInlineList(label string, items []string) api.Text {
	title := api.Text{Content: label + ": ", Style: "font-semibold"}
	return title.Add(api.Text{Content: strings.Join(items, ", ")})
}

func actionsToButtonGroup(actions []NotificationAction) api.ButtonGroup {
	buttons := make([]api.Button, 0, len(actions))
	for i, action := range actions {
		if action.URL == "" || action.Label == "" {
			continue
		}
		variant := action.Style
		if variant == "" && i == 0 {
			variant = "primary"
		}
		buttons = append(buttons, api.Button{
			Label:   action.Label,
			Href:    action.URL,
			Variant: variant,
		})
	}
	return api.ButtonGroup{Buttons: buttons}
}

func dodStatus(eventName string) string {
	if strings.Contains(eventName, "passed") {
		return "passed"
	}
	return "regressed"
}

func mapLabels(labels map[string]string) map[string]string {
	if len(labels) == 0 {
		return nil
	}
	out := make(map[string]string, len(labels))
	for k, v := range labels {
		if strings.TrimSpace(v) == "" {
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mapLabelsFromJSONStringMap(labels types.JSONStringMap) map[string]string {
	if len(labels) == 0 {
		return nil
	}
	return mapLabels(map[string]string(labels))
}

func mapLabelsFromJSONStringMapPtr(labels *types.JSONStringMap) map[string]string {
	if labels == nil {
		return nil
	}
	return mapLabelsFromJSONStringMap(*labels)
}

func labelFieldsFromLabels(labels []models.Label) []NotificationField {
	if len(labels) == 0 {
		return nil
	}
	fields := make([]NotificationField, 0, len(labels))
	for _, label := range labels {
		if strings.TrimSpace(label.Value) == "" {
			continue
		}
		fields = append(fields, field(label.Key, label.Value))
	}
	return fields
}

func labelFieldsFromMap(labels map[string]string) []NotificationField {
	if len(labels) == 0 {
		return nil
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	fields := make([]NotificationField, 0, len(keys))
	for _, key := range keys {
		fields = append(fields, field(key, labels[key]))
	}
	return fields
}

func coalesceString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func slackEmojiStatus(health models.Health) string {
	switch health {
	case models.HealthHealthy:
		return ":large_green_circle:"
	case models.HealthUnhealthy:
		return ":red_circle:"
	case models.HealthWarning:
		return ":large_orange_circle:"
	case models.HealthUnknown:
		return ":white_circle:"
	default:
		return ":white_circle:"
	}
}

func safeName(value string) string {
	if strings.TrimSpace(value) == "" {
		return "unknown"
	}
	return value
}

func slackRecentEventsText(events []string) string {
	return fmt.Sprintf("*Recent Events:* %s", strings.Join(events, ", "))
}

func slackDescriptionStyle(eventName string) string {
	switch eventName {
	case icapi.EventConfigHealthy,
		icapi.EventConfigUnhealthy,
		icapi.EventConfigWarning,
		icapi.EventConfigUnknown,
		icapi.EventConfigDegraded,
		icapi.EventConfigCreated,
		icapi.EventConfigUpdated,
		icapi.EventConfigDeleted,
		icapi.EventConfigChanged,
		icapi.EventComponentHealthy,
		icapi.EventComponentUnhealthy,
		icapi.EventComponentWarning,
		icapi.EventComponentUnknown:
		return "slack-plain"
	default:
		return "slack-section"
	}
}

func slackGroupedResourcesText(payload NotificationMessagePayload) string {
	if len(payload.GroupedResources) == 0 {
		return ""
	}

	switch payload.EventName {
	case icapi.EventCheckFailed, icapi.EventCheckPassed:
		return fmt.Sprintf("*Resources grouped with notification:* %s", strings.Join(payload.GroupedResources, "\n"))
	default:
		if payload.GroupedResourcesTitle != "" {
			return fmt.Sprintf("*%s:* - %s", payload.GroupedResourcesTitle, strings.Join(payload.GroupedResources, "\n - "))
		}
		return fmt.Sprintf("*Also Failing:* - %s", strings.Join(payload.GroupedResources, "\n - "))
	}
}

func stringValue(value any) string {
	return fmt.Sprintf("%v", value)
}

func healthValue(value *models.Health) models.Health {
	if value == nil {
		return models.HealthUnknown
	}
	return *value
}

func stringPtr(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
