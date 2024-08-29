package notification

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/slack-go/slack"
)

type SlackMsgTemplate struct {
	Blocks slack.Blocks `json:"blocks"`
}

func SlackSend(ctx *Context, apiToken, channel string, msg NotificationTemplate) error {
	api := slack.New(apiToken)

	var opts []slack.MsgOption
	if msg.Title != "" {
		opts = append(opts, slack.MsgOptionText(msg.Title, false))
	}

	if msg.Message != "" {
		if strings.Contains(msg.Message, `"blocks"`) {
			var slackMsg SlackMsgTemplate
			if err := json.Unmarshal([]byte(msg.Message), &slackMsg); err != nil {
				return fmt.Errorf("failed to unmarshal slack template into blocks: %w", err)
			}

			opts = append(opts, slack.MsgOptionBlocks(slackMsg.Blocks.BlockSet...))
		} else {
			opts = append(opts, slack.MsgOptionText(msg.Message, false))
		}
	}

	_, _, err := api.PostMessage(channel, opts...)
	return err
}
