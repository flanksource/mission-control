package events

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/sethvargo/go-retry"
	"gorm.io/gorm"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/responder"
)

const (
	EventResponderCreate = "responder.create"
	EventCommentCreate   = "comment.create"
	EventPushQueueCreate = "push_queue.create"
)

const (
	eventMaxAttempts      = 3
	waitDurationOnFailure = time.Minute
	pgNotifyTimeout       = time.Minute

	dbReconnectMaxDuration         = time.Minute * 5
	dbReconnectBackoffBaseDuration = time.Second
)

type Config struct {
	UpstreamConf api.UpstreamConfig
}

type eventHandler struct {
	ctx    context.Context
	gormDB *gorm.DB
	config Config
}

func NewEventHandler(ctx context.Context, gormDB *gorm.DB, config Config) *eventHandler {
	return &eventHandler{
		ctx:    ctx,
		gormDB: gormDB,
		config: config,
	}
}

func (t *eventHandler) ListenForEvents() {
	logger.Infof("started listening for database notify events")

	// Consume pending events
	t.ConsumeEventsUntilEmpty()

	pgNotify := make(chan bool)
	go t.listenToPostgresNotify(pgNotify)

	for {
		select {
		case <-pgNotify:
			t.ConsumeEventsUntilEmpty()

		case <-time.After(pgNotifyTimeout):
			t.ConsumeEventsUntilEmpty()
		}
	}
}

// listenToPostgresNotify listens to postgres notifications.
// It will retry on failure for dbReconnectMaxAttempt times.
func (t *eventHandler) listenToPostgresNotify(pgNotify chan bool) {
	var listen = func(ctx context.Context, pgNotify chan bool) error {
		conn, err := db.Pool.Acquire(ctx)
		if err != nil {
			return fmt.Errorf("error acquiring database connection: %v", err)
		}
		defer conn.Release()

		_, err = conn.Exec(ctx, "LISTEN event_queue_updates")
		if err != nil {
			return fmt.Errorf("error listening to database notifications: %v", err)
		}
		logger.Infof("listening to database notifications")

		for {
			notification, err := conn.Conn().WaitForNotification(ctx)
			if err != nil {
				return fmt.Errorf("error listening to database notifications: %v", err)
			}

			logger.Debugf("Received database notification: %+v", notification)
			pgNotify <- true
		}
	}

	// retry on failure.
	for {
		backoff := retry.WithMaxDuration(dbReconnectMaxDuration, retry.NewExponential(dbReconnectBackoffBaseDuration))
		err := retry.Do(t.ctx, backoff, func(ctx context.Context) error {
			if err := listen(ctx, pgNotify); err != nil {
				return retry.RetryableError(err)
			}

			return nil
		})

		logger.Errorf("failed to connect to database: %v", err)
	}
}

func (t *eventHandler) consumeEvents() error {
	tx := t.gormDB.WithContext(t.ctx).Begin()
	if tx.Error != nil {
		return fmt.Errorf("error initiating db tx: %w", tx.Error)
	}

	const selectEventsQuery = `
		SELECT * FROM event_queue
		WHERE attempts <= @maxAttempts OR ((now() - last_attempt) > '1 hour'::interval)
		FOR UPDATE SKIP LOCKED
		LIMIT 1
	`

	var event api.Event
	err := tx.Raw(selectEventsQuery, map[string]any{"maxAttempts": eventMaxAttempts}).First(&event).Error
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
	case EventPushQueueCreate:
		if t.config.UpstreamConf.Valid() {
			upstreamPushEventHandler := newPushToUpstreamEventHandler(t.config.UpstreamConf)
			err = upstreamPushEventHandler.Run(t.ctx, tx, []api.Event{event})
		}
	default:
		logger.Errorf("unrecognized event name: %s", event.Name)
		return tx.Commit().Error
	}

	if err != nil {
		logger.Errorf("failed to handle event [%s]: %v", event.Name, err)

		errorMsg := err.Error()
		setErr := tx.Exec("UPDATE event_queue SET error = ?, attempts = attempts + 1 WHERE id = ?", errorMsg, event.ID).Error
		if setErr != nil {
			logger.Errorf("error updating table:event_queue with id:%s and error:%s. %v", event.ID, errorMsg, setErr)
		}
		return tx.Commit().Error
	}

	err = tx.Delete(&event).Error
	if err != nil {
		logger.Errorf("error deleting event from event_queue table with id:%s", event.ID.String())
		return tx.Rollback().Error
	}
	return tx.Commit().Error
}

// ConsumeEventsUntilEmpty consumes events forever until the event queue is empty.
func (t *eventHandler) ConsumeEventsUntilEmpty() {
	for {
		err := t.consumeEvents()
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return
			} else {
				logger.Errorf("error processing event, waiting 60s to try again %v", err)
				time.Sleep(waitDurationOnFailure)
			}
		}
	}
}

func reconcileResponderEvent(tx *gorm.DB, event api.Event) error {
	responderID := event.Properties["id"]
	ctx := api.NewContext(tx)

	var _responder api.Responder
	err := tx.Where("id = ? AND external_id is NULL", responderID).Preload("Team").Find(&_responder).Error
	if err != nil {
		return err
	}

	responder, err := responder.GetResponder(ctx, _responder.Team)
	if err != nil {
		return err
	}
	externalID, err := responder.NotifyResponder(ctx, _responder)
	if err != nil {
		return err
	}
	if externalID != "" {
		// Update external id in responder table
		return tx.Model(&api.Responder{}).Where("id = ?", _responder.ID).Update("external_id", externalID).Error
	}

	return nil
}

func reconcileCommentEvent(tx *gorm.DB, event api.Event) error {
	commentID := event.Properties["id"]
	commentBody := event.Properties["body"]
	ctx := api.NewContext(tx)

	var err error
	var comment api.Comment
	query := tx.Where("id = ? AND external_id IS NULL", commentID).First(&comment)
	if query.Error != nil {
		if errors.Is(query.Error, gorm.ErrRecordNotFound) {
			logger.Debugf("Skipping comment %s since it was added via responder", commentID)
			return nil
		}
		return query.Error
	}

	// Get all responders related to a comment
	var responders []api.Responder
	commentRespondersQuery := `
        SELECT * FROM responders WHERE incident_id IN (
            SELECT incident_id FROM comments WHERE id = ?
        )
    `
	if err = tx.Raw(commentRespondersQuery, commentID).Preload("Team").Find(&responders).Error; err != nil {
		return err
	}

	// For each responder add the comment
	for _, _responder := range responders {
		// Reset externalID to avoid inserting previous iteration's ID
		externalID := ""

		team, err := responder.GetResponder(ctx, _responder.Team)
		if err != nil {
			return err
		}
		externalID, err = team.NotifyResponderAddComment(ctx, _responder, commentBody)

		if err != nil {
			// TODO: Associate error messages with responderType and handle specific responders when reprocessing
			logger.Errorf("error adding comment to responder:%s %v", _responder.Properties["responderType"], err)
			continue
		}

		// Insert into comment_responders table
		if externalID != "" {
			err = tx.Exec("INSERT INTO comment_responders (comment_id, responder_id, external_id) VALUES (?, ?, ?)",
				commentID, _responder.ID, externalID).Error
			if err != nil {
				logger.Errorf("error updating comment_responders table: %v", err)
			}
		}
	}

	return nil
}
