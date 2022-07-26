package events

import (
	"context"
	//"fmt"

	"github.com/flanksource/commons/logger"
	"gorm.io/gorm"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	responderPkg "github.com/flanksource/incident-commander/responder"
)

func ListenForEvents() error {

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

	// Consume pending events
	consumeEvents()

	for {

		// TODO: Fetch all at once then process or keep processing till count == 0
		// TODO: Change logic to select based as Moshe suggested
		_, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return err
		}

		consumeEvents()
	}
}

func consumeEvents() error {
	var event api.Event

	tx := db.Gorm.Begin()

	// TODO: Add attempts where clause
	err := tx.Raw("SELECT id, properties FROM event_queue FOR UPDATE SKIP LOCKED").Scan(&event).Error
	if err != nil {
		return err
	}

	if event.Name == "responder.create" {
		// TODO: Make this a goroutine ?
		err = reconcileResponderEvent(tx, event)
	}

	setErr := event.SetErrorMessage(err.Error())
	if setErr != nil {
		logger.Errorf("Error updating table:event_queue with id:%s and error:%s", event.ID, setErr)
		return tx.Rollback().Error
	}

	event.Done()
	return tx.Commit().Error
}

func reconcileResponderEvent(tx *gorm.DB, event api.Event) error {
	responderID := event.Properties["id"]

	var responder api.Responder
	err := tx.Find(&responder).Where("id = ? AND external_id is NULL", responderID).Preload("Team").Error
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
