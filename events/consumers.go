package events

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/api"
	"gorm.io/gorm"
)

var (
	ConsumerTeam         = []string{EventTeamUpdate, EventTeamDelete}
	ConsumerNotification = []string{EventNotificationSend, EventNotificationUpdate, EventNotificationDelete}
	ConsumerCheckStatus  = []string{EventCheckPassed, EventCheckFailed}
	ConsumerResponder    = []string{EventIncidentResponderAdded, EventIncidentCommentAdded}

	ConsumerIncidentNotification = []string{
		EventIncidentCreated, EventIncidentResponderAdded, EventIncidentResponderRemoved,
		EventIncidentDODAdded, EventIncidentDODPassed,
		EventIncidentDODRegressed, EventIncidentStatusOpen, EventIncidentStatusClosed,
		EventIncidentStatusMitigated, EventIncidentStatusResolved,
		EventIncidentStatusInvestigating, EventIncidentStatusCancelled,
	}
	ConsumerPushQueue = []string{EventPushQueueCreate}

	AllConsumers = []EventConsumer{
		NotificationConsumer,
		TeamConsumer,
		ResponderConsumer,
		UpstreamPushConsumer,
	}
)

type EventConsumer struct {
	WatchEvents []string
	HandleFunc  func(*api.Context, Config, api.Event) error
	BatchSize   int
	Consumers   int
	DB          *gorm.DB
	Config      Config
}

func (e EventConsumer) Validate() error {
	if e.BatchSize <= 0 {
		return fmt.Errorf("BatchSize:%d <= 0", e.BatchSize)
	}
	if e.Consumers <= 0 {
		return fmt.Errorf("Consumers:%d <= 0", e.BatchSize)
	}
	if len(e.WatchEvents) == 0 {
		return fmt.Errorf("Event to watch:%d <= 0", len(e.WatchEvents))
	}
	return nil
}

func (t *EventConsumer) consumeEvents() error {
	tx := t.DB.Begin()
	if tx.Error != nil {
		return fmt.Errorf("error initiating db tx: %w", tx.Error)
	}

	ctx := api.NewContext(tx, nil)

	const selectEventsQuery = `
        DELETE FROM event_queue
        WHERE id = (
            SELECT id FROM event_queue
            WHERE 
                attempts <= @maxAttempts AND
                name IN (@events)
            ORDER BY priority DESC, created_at ASC
            FOR UPDATE SKIP LOCKED
            LIMIT @batchSize
        )
        RETURNING *
	`

	var events []api.Event
	vals := map[string]any{
		"maxAttempts": eventMaxAttempts,
		"events":      strings.Join(t.WatchEvents, ","),
		"batchSize":   t.BatchSize,
	}
	err := tx.Raw(selectEventsQuery, vals).Scan(&events).Error
	if err != nil {
		// Rollback the transaction in case of no records found to prevent
		// creating dangling connections and to release the locks
		tx.Rollback()
		return err
	}

	var failedEvents []*api.Event
	for _, event := range events {
		err = t.HandleFunc(ctx, t.Config, event)
		if err != nil {
			logger.Errorf("failed to handle event [%s]: %v", event.Name, err)

			event.Attempts += 1
			event.Error = err.Error()
			last_attempt := time.Now()
			event.LastAttempt = &last_attempt
			failedEvents = append(failedEvents, &event)
		}
	}

	if err := tx.Create(failedEvents).Error; err != nil {
		// TODO: More robust way to handle failed event insertion failures
		logger.Errorf("Error inserting into table:event_queue with error:%v. %v", err)
	}
	return tx.Commit().Error
}

// ConsumeEventsUntilEmpty consumes events forever until the event queue is empty.
func (t *EventConsumer) ConsumeEventsUntilEmpty() {
	consumerFunc := func(wg *sync.WaitGroup) {
		for {
			err := t.consumeEvents()
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					wg.Done()
					return
				} else {
					logger.Errorf("error processing event, waiting 60s to try again %v", err)
					time.Sleep(waitDurationOnFailure)
				}
			}
		}
	}

	var wg sync.WaitGroup
	for i := 0; i < t.Consumers; i++ {
		wg.Add(1)
		go consumerFunc(&wg)
	}
	wg.Wait()
}

func (e *EventConsumer) Listen() {
	if err := e.Validate(); err != nil {
		logger.Fatalf("Error starting event consumer: %v", err)
		return
	}

	// Consume pending events
	e.ConsumeEventsUntilEmpty()

	for range time.Tick(time.Second * 30) {
		e.ConsumeEventsUntilEmpty()
	}
}

func (e *EventConsumer) SetConfig(config Config) {
	e.Config = config
}

func StartConsumers(config Config) {
	for _, c := range AllConsumers {
		c.SetConfig(config)
		go c.Listen()
	}
}
