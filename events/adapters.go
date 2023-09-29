package events

import (
	"errors"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/postq"
)

func AsyncAdapter(fn func(api.Context, postq.Events) postq.Events) postq.AsyncEventHandlerFunc {
	return func(ctx postq.Context, events postq.Events) postq.Events {
		c, ok := ctx.(api.Context)
		if !ok {
			for i := range events {
				events[i].SetError("invalid context")
			}

			return events
		}

		return fn(c, events)
	}
}

func SyncAdapter(fn func(api.Context, postq.Event) error) postq.SyncEventHandlerFunc {
	return func(ctx postq.Context, event postq.Event) error {
		c, ok := ctx.(api.Context)
		if !ok {
			return errors.New("invalid context")
		}

		return fn(c, event)
	}
}
