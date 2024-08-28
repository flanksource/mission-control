package notification

import (
	"encoding/json"
	"fmt"
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
