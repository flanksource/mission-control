package events

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/flanksource/commons/logger"
	"gorm.io/gorm"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	responderPkg "github.com/flanksource/incident-commander/responder"
)

const (
	EventResponderCreate = "responder.create"
	EventCommentCreate   = "comment.create"
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

		case <-time.After(60 * time.Second):
			consumeEvents()
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

		selectEventsQuery := `
			SELECT * FROM event_queue
			WHERE
				attempts <= 3 OR ((now() - last_attempt) > '1 hour'::interval)
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		`
		err := tx.Raw(selectEventsQuery).First(&event).Error
		if err != nil {
			// Commit the transaction in case of no records found to prevent
			// creating dangling connections and to release the locks
			tx.Commit()
			return err
		}

		switch event.Name {
		case EventResponderCreate:
			err = reconcileResponderEvent(tx, event)
		case EventCommentCreate:
			err = reconcileCommentEvent(tx, event)
		default:
			logger.Errorf("Invalid event name: %s", event.Name)
			return tx.Commit().Error
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
	switch responder.Properties["responderType"] {
	case responderPkg.JiraResponder:
		externalID, err = responderPkg.NotifyJiraResponder(responder)
	case responderPkg.MSPlannerResponder:
		externalID, err = responderPkg.NotifyMSPlannerResponder(responder)
	default:
		return fmt.Errorf("Invalid responder type: %s received", responder.Properties["responderType"])
	}

	if err != nil {
		return err
	}

	if externalID != "" {
		// Update external id in responder table
		return tx.Model(&api.Responder{}).Where("id = ?", responder.ID).Update("external_id", externalID).Error
	}

	return nil
}

func reconcileCommentEvent(tx *gorm.DB, event api.Event) error {
	commentID := event.Properties["id"]
	commentBody := event.Properties["body"]

	// Get all responders related to a comment
	var responders []api.Responder
	commentRespondersQuery := `
        SELECT * FROM responders WHERE incident_id IN (
            SELECT incident_id FROM comments WHERE id = ?
        )
    `
	var err error
	if err = tx.Raw(commentRespondersQuery, commentID).Preload("Team").Find(&responders).Error; err != nil {
		return err
	}

	// For each responder add the comment
	for _, responder := range responders {
		// Reset externalID to avoid inserting previous iteration's ID
		externalID := ""

		switch responder.Properties["responderType"] {
		case responderPkg.JiraResponder:
			externalID, err = responderPkg.NotifyJiraResponderAddComment(responder, commentBody)
		case responderPkg.MSPlannerResponder:
			externalID, err = responderPkg.NotifyMSPlannerResponderAddComment(responder, commentBody)
		default:
			continue
		}

		if err != nil {
			// TODO: Associate error messages with responderType and handle specific responders when reprocessing
			logger.Errorf("Error adding comment to responder:%s %v", responder.Properties["responderType"], err)
			continue
		}

		// Insert into comment_responders table
		if externalID != "" {
			err = tx.Exec("INSERT INTO comment_responders (comment_id, responder_id, external_id) VALUES (?, ?, ?)",
				commentID, responder.ID, externalID).Error
			if err != nil {
				logger.Errorf("Error updating comment_responders table: %v", err)
			}
		}
	}

	return nil
}
