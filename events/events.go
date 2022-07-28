package events

import (
	"context"
	"errors"
	"time"

	"github.com/flanksource/commons/logger"
	"gorm.io/gorm"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	responderPkg "github.com/flanksource/incident-commander/responder"
)

func ListenForEvents() {

	logger.Infof("Started listening for events")

	// Consume pending events
	consumeEvents()

	pgNotify := make(chan bool)
	go listenToPostgresNotify(pgNotify)

	for {
		select {
		case <-pgNotify:
			consumeEvents()

		case <-time.After(10 * time.Second):
			logger.Debugf("timed out waiting for pgNotify")
		}
	}
}

func listenToPostgresNotify(pgNotify chan bool) {
	ctx := context.Background()

	conn, err := db.Pool.Acquire(ctx)
	if err != nil {
		logger.Errorf("Error creating database pool: %v", err)
	}

	_, err = conn.Exec(ctx, "LISTEN event_queue_updates")
	if err != nil {
		logger.Errorf("Error listening to database notify: %v", err)
	}

	for {
		_, err = conn.Conn().WaitForNotification(ctx)
		if err != nil {
			logger.Errorf("Error waiting for database notifications: %v", err)
		}

		pgNotify <- true
	}

}

func consumeEvents() {

	consumeEvent := func() error {
		var event api.Event

		tx := db.Gorm.Begin()

		// TODO: Add attempts where clause
		err := tx.Raw("SELECT id, name, properties FROM event_queue FOR UPDATE SKIP LOCKED LIMIT 1").First(&event).Error
		if err != nil {
			return err
		}

		if event.Name == "responder.create" {
			err = reconcileResponderEvent(tx, event)
		}

		if err != nil {
			errorMsg := err.Error()
			setErr := tx.Exec("UPDATE event_queue SET error = ?, attempts = attempts + 1 WHERE id = ?", errorMsg, event.ID).Error
			if setErr != nil {
				logger.Errorf("Error updating table:event_queue with id:%s and error:%s. %v", event.ID, errorMsg, setErr)
			}
			return tx.Commit().Error
		}

		err = tx.Delete(&event).Error
		if err != nil {
			logger.Errorf("Error deleting event from event_queue table with id:%s", event.ID.String())
			return tx.Rollback().Error
		}
		return tx.Commit().Error

	}

	// Keep on iterating till the queue is empty
	for {
		err := consumeEvent()
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return
			} else {
				logger.Errorf("Error processing event: %v", err)
			}
		}
	}
}

func reconcileResponderEvent(tx *gorm.DB, event api.Event) error {
	responderID := event.Properties["id"]

	var responder api.Responder
	err := tx.Where("id = ? AND external_id is NULL", responderID).Preload("Team").Find(&responder).Error
	if err != nil {
		return err
	}

	var externalID string
	if responder.Properties["responderType"] == "Jira" {
		externalID, err = responderPkg.NotifyJiraResponder(responder)
		if err != nil {
			return err
		}
	}

	if externalID != "" {
		// Update external id in responder table
		return tx.Model(&api.Responder{}).Where("id = ?", responder.ID).Update("external_id", externalID).Error
	}

	return nil
}
