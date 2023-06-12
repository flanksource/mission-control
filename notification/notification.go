package notification

import "github.com/flanksource/incident-commander/api"

type Notifier interface {
	// NotifyResponderAdded sends notifications about a new responder on an incident.
	NotifyResponderAdded(ctx *api.Context, responder api.Responder) error
}
