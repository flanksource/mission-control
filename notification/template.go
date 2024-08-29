package notification

import (
	"encoding/json"
	"fmt"

	"github.com/flanksource/duty/models"
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

		var fields []any
		for k, v := range labels {
			fields = append(fields, map[string]any{
				"type":     "mrkdwn",
				"text":     fmt.Sprintf("*%s*: %s", k, v),
				"verbatim": true,
			})
		}

		var m = map[string]any{
			"type":   "section",
			"fields": fields,
			"text": map[string]any{
				"type": "mrkdwn",
				"text": "*Labels*",
			},
		}

		out, _ := json.Marshal(m)
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
