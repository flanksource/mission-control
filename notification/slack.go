package notification

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/slack-go/slack"
)

type SlackMsgTemplate struct {
	Blocks slack.Blocks `json:"blocks"`
}

func SlackSend(ctx *Context, apiToken, channel string, msg NotificationTemplate) error {
	if channel == "" {
		return errors.New("slack channel cannot be empty")
	}

	api := slack.New(apiToken)

	var opts []slack.MsgOption
	if msg.Title != "" {
		opts = append(opts, slack.MsgOptionText(msg.Title, false))
	}

	// keep track of the message body for notification send history.
	// we can't JSON marshal opts (type []slack.MsgOption)
	var msgBody []any

	if msg.Message != "" {
		if strings.Contains(msg.Message, `"blocks"`) {
			var slackMsg SlackMsgTemplate
			if err := json.Unmarshal([]byte(msg.Message), &slackMsg); err != nil {
				return fmt.Errorf("failed to unmarshal slack template into blocks: %w", err)
			}

			opts = append(opts, slack.MsgOptionBlocks(slackMsg.Blocks.BlockSet...))
			msgBody = append(msgBody, slackMsg)
		} else {
			opts = append(opts, slack.MsgOptionText(msg.Message, false))
			msgBody = append(msgBody, msg.Message)
		}
	}

	if b, err := json.Marshal(msgBody); err != nil {
		ctx.WithMessage(msg.Message)
	} else {
		ctx.WithMessage(string(b))
	}

	_, _, err := api.PostMessageContext(ctx, channel, opts...)

	var slackError slack.SlackErrorResponse
	if errors.As(err, &slackError) {
		switch slackError.Err {
		case "channel_not_found":
			return fmt.Errorf("slack channel %q not found. ensure the channel exists & the bot has permission on that channel", channel)
		}
	}

	return ctx.Oops().Hint(msg.Message).Wrap(err)
}
