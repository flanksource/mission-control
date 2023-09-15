package events

import (
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/utils"
	"github.com/jackc/pgx/v5/pgxpool"
)

// pgNotifyRouter distributes the pgNotify event to multiple channels
// based on the payload.
type pgNotifyRouter struct {
	pgpool   *pgxpool.Pool
	registry map[string]chan string
}

func newPgNotifyRouter(pgpool *pgxpool.Pool) *pgNotifyRouter {
	return &pgNotifyRouter{
		pgpool:   pgpool,
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

func (t *pgNotifyRouter) Run(channel string) {
	const (
		dbReconnectMaxDuration         = time.Minute * 5
		dbReconnectBackoffBaseDuration = time.Second
	)

	eventQueueNotifyChannel := make(chan string)
	go utils.ListenToPostgresNotify(t.pgpool, channel, dbReconnectMaxDuration, dbReconnectBackoffBaseDuration, eventQueueNotifyChannel)

	logger.Infof("running pg notify router")
	for payload := range eventQueueNotifyChannel {
		if ch, ok := t.registry[payload]; ok {
			ch <- payload
		} else if payload != "push_queue.create" { // Ignore push queue events cuz that'll pollute the logs
			logger.Warnf("notify router:: received notification for an unregistered event: %s", payload)
		}
	}
}
