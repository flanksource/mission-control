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
	consumer := upstream.NewPushUpstreamConsumer(api.UpstreamConf)
	events.RegisterAsyncHandler(consumer, 50, 5, api.EventPushQueueCreate)
}
