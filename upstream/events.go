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
	deleteConsumer := upstream.NewDeleteFromUpstreamConsumer(api.UpstreamConf)
	events.RegisterAsyncHandler(deleteConsumer, 100, 10, upstream.EventPushQueueDelete)
}
