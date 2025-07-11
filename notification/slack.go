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

const DefaultLabelsWhitelist = `app|batch.kubernetes.io/jobname|app.kubernetes.io/name|kustomize.toolkit.fluxcd.io/name;
app.kubernetes.io/version;
`

// TrimLabels returns a subset of labels that match the whitelist.
// The whitelist contains a set of groups separated by semicolons
// with group members separated by pipes.
func TrimLabels(whitelist string, labels map[string]string) map[string]string {
	groups := strings.Split(whitelist, ";")
	matchedLabels := make(map[string]string)

	for _, group := range groups {
		for key := range strings.SplitSeq(group, "|") {
			key = strings.TrimSpace(key)
			if val, ok := labels[key]; ok {
				matchedLabels[key] = val
				break // move to the next group
			}
		}
	}

	return matchedLabels
}
