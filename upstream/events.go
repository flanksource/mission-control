package upstream

import (
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/upstream"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/events"
)

func init() {
	events.Register(RegisterEvents)
}

func RegisterEvents(ctx context.Context) {
	pushConsumer := upstream.NewPushUpstreamConsumer(api.UpstreamConf)
	events.RegisterAsyncHandler(pushConsumer, 50, 5, upstream.EventPushQueueCreate)

	deleteConsumer := upstream.NewDeleteFromUpstreamConsumer(api.UpstreamConf)
	events.RegisterAsyncHandler(deleteConsumer, 50, 5, upstream.EventPushQueueDelete)
}
