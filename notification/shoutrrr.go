package notification

import (
	"fmt"

	"github.com/containrrr/shoutrrr"
	"github.com/containrrr/shoutrrr/pkg/types"
	"github.com/flanksource/incident-commander/api"
)

func Publish(ctx *api.Context, connectionName, url, message string, properties map[string]string) error {
	if connectionName != "" {
		connection, err := ctx.HydrateConnection(connectionName)
		if err != nil {
			return err
		}
		url = connection.URL
	}

	sender, err := shoutrrr.CreateSender(url)
	if err != nil {
		return fmt.Errorf("failed to create a shoutrrr sender client: %w", err)
	}

	var params *types.Params
	if properties != nil {
		params = (*types.Params)(&properties)
	}

	sendErrors := sender.Send(message, params)
	for _, err := range sendErrors {
		if err != nil {
			return fmt.Errorf("error publishing notification: %w", err)
		}
	}

	return nil
}
