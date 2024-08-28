package notification

import "github.com/slack-go/slack"

func SlackSend(ctx *Context, apiToken, channel string, msg NotificationTemplate) error {
	api := slack.New(apiToken)
	_, _, err := api.PostMessage(channel, slack.MsgOptionText(msg.Message, false))
	return err
}
