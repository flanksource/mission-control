package events

import (
	"context"
	//"fmt"

	"github.com/flanksource/commons/logger"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/responder"
)

func ProcessQueue() error {

	ctx := context.Background()

	conn, err := db.Pool.Acquire(ctx)
	if err != nil {
		return err
	}

	_, err = conn.Exec(ctx, "LISTEN event_queue_updates")
	if err != nil {
		return err
	}

	logger.Infof("Started event processor")

	// Reconcile pending events
	reconcileEvents()

	for {
		_, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return err
		}

		reconcileEvents()

	}
}

func reconcileEvents() error {
	// TODO: Add attempts where clause
	var events []api.Event
	tx := db.Gorm.Raw("SELECT id, properties FROM event_queue FOR UPDATE SKIP LOCKED").Scan(&events)
	if tx.Error != nil {
		return tx.Error
	}

	var responderEvents []api.Event
	for _, event := range events {
		if event.Properties["type"] == "responder" {
			responderEvents = append(responderEvents, event)
		}
	}

	if len(responderEvents) > 0 {
		responder.ReconcileEvents(responderEvents)
	}

	return nil
}
