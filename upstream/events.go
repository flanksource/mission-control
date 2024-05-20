package upstream

import (
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/upstream"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/events"
	"github.com/flanksource/postq"
)

func init() {
	events.Register(RegisterEvents)
}

func RegisterEvents(ctx context.Context) {
	if api.UpstreamConf.Valid() {
		deleteConsumer := upstream.NewDeleteFromUpstreamConsumer(api.UpstreamConf)
		events.RegisterAsyncHandler(deleteConsumer, 100, 10, upstream.EventPushQueueDelete)
		return
	}

	// Noop async handler on upstream servers so we clear out the delete events.
	// This shouldn't be necessary once we handle
	// https://github.com/flanksource/mission-control/issues/1023
	events.RegisterAsyncHandler(func(ctx context.Context, e postq.Events) postq.Events {
		return nil
	}, 500, 1, upstream.EventPushQueueDelete)
}
