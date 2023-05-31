package notification

import "github.com/flanksource/incident-commander/api"

type INotifier interface {
	// NotifyResponderAdded sends notifications about new responder
	NotifyResponderAdded(ctx *api.Context, responder api.Responder) error
}
