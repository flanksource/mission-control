package events

import (
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/duty/pg"
	"github.com/flanksource/incident-commander/api"
)

// pgNotifyRouter distributes the pgNotify event to multiple channels
// based on the payload.
type pgNotifyRouter struct {
	registry map[string]chan string
}

func newPgNotifyRouter() *pgNotifyRouter {
	return &pgNotifyRouter{
		registry: make(map[string]chan string),
	}
}

// RegisterRoutes creates a single channel for the given routes and returns it.
func (t *pgNotifyRouter) RegisterRoutes(routes []string) <-chan string {
	pgNotifyChannel := make(chan string)
	for _, we := range routes {
		t.registry[we] = pgNotifyChannel
	}
	return pgNotifyChannel
}

func (t *pgNotifyRouter) Run(ctx api.Context, channel string) {
	eventQueueNotifyChannel := make(chan string)
	go pg.Listen(ctx, channel, eventQueueNotifyChannel)

	logger.Debugf("running pg notify router")
	for payload := range eventQueueNotifyChannel {
		if ch, ok := t.registry[payload]; ok {
			ch <- payload
		} else if payload != EventPushQueueCreate { // Ignore push queue events cuz that'll pollute the logs
			logger.Warnf("notify router:: received notification for an unregistered event: %s", payload)
		}
	}
}
