package notification

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/samber/lo"
)

var TemplateFuncs = map[string]any{
	"labelsFormat": func(labels map[string]any) string {
		if len(labels) == 0 {
			return ""
		}

		output := "### Labels:\n"
		for k, v := range labels {
			output += fmt.Sprintf("**%s**: %s \n", k, v)
		}

		return output
	},
	"slackHealthEmoji": func(health string) string {
		switch models.Health(health) {
		case models.HealthHealthy:
			return ":large_green_circle:"
		case models.HealthUnhealthy:
			return ":red_circle:"
		case models.HealthWarning:
			return ":large_orange_circle:"
		default:
			return ":white_circle:"
		}
	},
	"slackSectionLabels": func(resource map[string]any) string {
		if resource == nil {
			return ""
		}

		var dummyResource models.ConfigItem
		if tagsRaw, ok := resource["tags"]; ok {
			if tags, ok := tagsRaw.(map[string]any); ok {
				dummyResource.Tags = make(map[string]string)
				for k, v := range tags {
					dummyResource.Tags[k] = v.(string)
				}
			}
		}

		if labelsRaw, ok := resource["labels"]; ok {
			if labels, ok := labelsRaw.(map[string]any); ok {
				labelsMap := types.JSONStringMap{}
				for k, v := range labels {
					labelsMap[k] = v.(string)
				}
				dummyResource.Labels = &labelsMap
			}
		}

		fields := dummyResource.GetTrimmedLabels()
		slackFields := CreateSlackFieldsSection(fields)

		if len(slackFields) == 0 {
			return ""
		}

		out, err := json.Marshal(slackFields)
		if err != nil {
			return ""
		}

		return string(out)
	},
	"slackSectionTextFieldPlain": func(text string) string {
		return fmt.Sprintf(`{
			"type": "plain_text",
			"text": %q
		}`, text)
	},
	"slackSectionTextFieldMD": func(text string) string {
		return fmt.Sprintf(`{
			"type": "mrkdwn",
			"text": %q
		}`, text)
	},
	"slackSectionTextMD": func(text string) string {
		return fmt.Sprintf(`{
			"type": "section",
			"text": {
				"type": "mrkdwn",
				"text": %q
			}
		}`, text)
	},
	"slackSectionTextPlain": func(text string) string {
		return fmt.Sprintf(`{
			"type": "section",
			"text": {
				"type": "plain_text",
				"text": %q
			}
		}`, text)
	},
	"slackURLAction": func(val ...string) string {
		if len(val)%2 != 0 {
			return "slackURLAction received an uneven pair of the action name and url"
		}

		var elements []string
		for i, pair := range lo.Chunk(val, 2) {
			name, url := pair[0], pair[1]

			var buttonStyle string
			if i == 0 {
				buttonStyle = "primary"
			}

			elements = append(elements, fmt.Sprintf(`
				{
					"type": "button",
					"text": {
						"type": "plain_text",
						"text": "%s",
						"emoji": true
					},
					"url": "%s",
					"action_id": "%s",
					"style": "%s"
				}`, name, url, name, buttonStyle))
		}

		return fmt.Sprintf(`{"type": "actions", "elements": [%s]}`, strings.Join(elements, ","))
	},
}

const (
	maxSlackFieldsPerSection = 10 // Slack doesn't support more than 10 fields in a section

	slackBlockTypeMarkdown = "mrkdwn"
	slackBlockTypeSection  = "section"
)

// CreateSlackFieldsSection creates a Slack section block with fields from a sorted list of labels.
func CreateSlackFieldsSection(labels []models.Label) map[string]any {
	if len(labels) == 0 {
		return nil
	}

	var fields []map[string]any
	count := 0
	for _, l := range labels {
		if count >= maxSlackFieldsPerSection {
			break
		}

		if strings.TrimSpace(l.Value) == "" {
			continue
		}

		fields = append(fields, map[string]any{
			"type":     slackBlockTypeMarkdown,
			"text":     fmt.Sprintf("*%s*: %s", l.Key, l.Value),
			"verbatim": true,
		})
		count++
	}

	if len(fields) == 0 {
		return nil
	}

	section := map[string]any{
		"type":   slackBlockTypeSection,
		"fields": fields,
	}

	return section
}
