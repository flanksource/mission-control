package notification

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/flanksource/duty/models"
	"github.com/samber/lo"
)

var templateFuncs = map[string]any{
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

		configTags := map[string]any{}
		var tagFields []map[string]any
		if tagsRaw, ok := resource["tags"]; ok {
			if tags, ok := tagsRaw.(map[string]any); ok {
				configTags = tags
				for k, v := range tags {
					tagFields = append(tagFields, map[string]any{
						"type":     "mrkdwn",
						"text":     fmt.Sprintf("*%s*: %s", k, v),
						"verbatim": true,
					})
				}
			}
		}

		var labelFields []map[string]any
		if labelsRaw, ok := resource["labels"]; ok {
			if labels, ok := labelsRaw.(map[string]any); ok {
				for k, v := range labels {
					if _, ok := configTags[k]; ok {
						continue // Already pulled from tags
					}

					labelFields = append(labelFields, map[string]any{
						"type":     "mrkdwn",
						"text":     fmt.Sprintf("*%s*: %s", k, v),
						"verbatim": true,
					})
				}
			}
		}

		if len(tagFields) == 0 && len(labelFields) == 0 {
			return ""
		}

		slices.SortFunc(tagFields, func(a, b map[string]any) int {
			return strings.Compare(a["text"].(string), b["text"].(string))
		})

		slices.SortFunc(labelFields, func(a, b map[string]any) int {
			return strings.Compare(a["text"].(string), b["text"].(string))
		})

		fields := append(tagFields, labelFields...)

		var outputs []string
		const maxFieldsPerSection = 10
		for i, chunk := range lo.Chunk(fields, maxFieldsPerSection) {
			var m = map[string]any{
				"type":   "section",
				"fields": chunk,
			}

			if i == 0 {
				m["text"] = map[string]any{
					"type": "mrkdwn",
					"text": "*Labels*",
				}
			}

			out, err := json.Marshal(m)
			if err == nil {
				outputs = append(outputs, string(out))
			}
		}

		return strings.Join(outputs, ",")
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
