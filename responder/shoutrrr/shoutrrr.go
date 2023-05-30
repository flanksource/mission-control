package shoutrrr

import (
	"fmt"

	"github.com/containrrr/shoutrrr"
	"github.com/containrrr/shoutrrr/pkg/router"
	"github.com/flanksource/incident-commander/api"
)

func NewClient(ctx *api.Context, team api.Team) (*ShoutrrrClient, error) {
	teamSpec, err := team.GetSpec()
	if err != nil {
		return nil, err
	}
	shoutrrrConfig := teamSpec.ResponderClients.NotificationClients

	senders := make([]*router.ServiceRouter, 0, len(shoutrrrConfig))
	for _, conf := range shoutrrrConfig {
		sender, err := shoutrrr.CreateSender(conf.URL)
		if err != nil {
			return nil, fmt.Errorf("failed to create a shoutrrr sender client: %w", err)
		}

		senders = append(senders, sender)
	}

	return &ShoutrrrClient{
		senders: senders,
	}, nil
}

type ShoutrrrClient struct {
	senders []*router.ServiceRouter
}

func (t *ShoutrrrClient) NotifyResponderAddComment(ctx *api.Context, responder api.Responder, comment string) (string, error) {
	msg := fmt.Sprintf("Someone commented on the issue: %s", comment)

	for _, sender := range t.senders {
		// TODO: Add filtering with cel expression

		errors := sender.Send(msg, nil)
		if errors != nil {
			return "", fmt.Errorf("failed to send messages: %v", errors)
		}
	}

	return "", nil
}

func (t *ShoutrrrClient) NotifyResponder(ctx *api.Context, responder api.Responder) (string, error) {
	// TODO: Need to finalize what needs to be sent here
	msg := fmt.Sprintf("Someone commented on the issue: %s", "TEST")

	for _, sender := range t.senders {
		// TODO: Add filtering with cel expression

		errors := sender.Send(msg, nil)
		if errors != nil {
			return "", fmt.Errorf("failed to send messages: %v", errors)
		}
	}

	return "", nil
}
