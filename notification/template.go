package notification

import (
	"fmt"
)

var templateFuncs = map[string]any{
	"labelsFormat": func(field map[string]any) string {
		if len(field) == 0 {
			return ""
		}

		output := "### Labels:\n"
		for k, v := range field {
			output += fmt.Sprintf("**%s**: %s \n", k, v)
		}

		return output
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
}
