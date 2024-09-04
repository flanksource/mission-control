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
	"slackSectionLabels": func(labels map[string]any) string {
		if len(labels) == 0 {
			return ""
		}

		var fields []map[string]any
		for k, v := range labels {
			fields = append(fields, map[string]any{
				"type":     "mrkdwn",
				"text":     fmt.Sprintf("*%s*: %s", k, v),
				"verbatim": true,
			})
		}

		slices.SortFunc(fields, func(a, b map[string]any) int {
			return strings.Compare(a["text"].(string), b["text"].(string))
		})

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
	"slackURLAction": func(name, url string) string {
		return fmt.Sprintf(`{
			"type": "actions",
			"elements": [
				{
					"type": "button",
					"text": {
						"type": "plain_text",
						"text": "%s",
						"emoji": true
					},
					"url": "%s",
					"action_id": "%s"
				}
			]
		}`, name, url, name)
	},
}
