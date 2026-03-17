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

// IsSlackBlocksJSON returns true when message is a JSON object with a top-level blocks array.
func IsSlackBlocksJSON(message string) bool {
	message = strings.TrimSpace(message)
	if message == "" {
		return false
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(message), &raw); err != nil {
		return false
	}

	blocks, ok := raw["blocks"]
	if !ok {
		return false
	}

	var blockArray []json.RawMessage
	return json.Unmarshal(blocks, &blockArray) == nil
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

	if msg.Message != "" {
		if IsSlackBlocksJSON(msg.Message) {
			var slackMsg SlackMsgTemplate
			if err := json.Unmarshal([]byte(msg.Message), &slackMsg); err != nil {
				return fmt.Errorf("failed to unmarshal slack template into blocks: %w", err)
			}

			opts = append(opts, slack.MsgOptionBlocks(slackMsg.Blocks.BlockSet...))
		} else {
			opts = append(opts, slack.MsgOptionText(msg.Message, false))
		}
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
