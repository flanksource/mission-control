package events

import (
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/api"
	pkgResponder "github.com/flanksource/incident-commander/responder"
	"github.com/flanksource/postq"
)

func NewResponderConsumerSync() postq.SyncEventConsumer {
	return postq.SyncEventConsumer{
		WatchEvents: []string{EventIncidentResponderAdded},
		Consumers:   postq.SyncHandlers(addNotificationEvent, generateResponderAddedAsyncEvent),
		ConsumerOption: &postq.ConsumerOption{
			ErrorHandler: defaultLoggerErrorHandler,
		},
	}
}

func NewCommentConsumerSync() postq.SyncEventConsumer {
	return postq.SyncEventConsumer{
		WatchEvents: []string{EventIncidentCommentAdded},
		Consumers:   postq.SyncHandlers(addNotificationEvent, generateCommentAddedAsyncEvent),
		ConsumerOption: &postq.ConsumerOption{
			ErrorHandler: defaultLoggerErrorHandler,
		},
	}
}

func NewResponderConsumerAsync() postq.AsyncEventConsumer {
	return postq.AsyncEventConsumer{
		WatchEvents: []string{EventJiraResponderAdded, EventMSPlannerResponderAdded, EventMSPlannerCommentAdded, EventJiraCommentAdded},
		Consumer:    postq.AsyncHandler(processResponderEvents),
		BatchSize:   1,
	}
}

// generateResponderAddedAsyncEvent generates async events for each of the configured responder clients
// in the associated team.
func generateResponderAddedAsyncEvent(ctx api.Context, event postq.Event) error {
	responderID := event.Properties["id"]

	var responder api.Responder
	err := ctx.DB().Where("id = ? AND external_id is NULL", responderID).Preload("Incident").Preload("Team").Find(&responder).Error
	if err != nil {
		return err
	}

	spec, err := responder.Team.GetSpec()
	if err != nil {
		return err
	}

	if spec.ResponderClients.Jira != nil {
		if err := ctx.DB().Clauses(eventQueueOnConflictClause).Create(&api.Event{Name: EventJiraResponderAdded, Properties: map[string]string{"id": responderID}}).Error; err != nil {
			return err
		}
	}

	if spec.ResponderClients.MSPlanner != nil {
		if err := ctx.DB().Clauses(eventQueueOnConflictClause).Create(&api.Event{Name: EventMSPlannerResponderAdded, Properties: map[string]string{"id": responderID}}).Error; err != nil {
			return err
		}
	}

	return nil
}

// generateCommentAddedAsyncEvent generates comment.add async events for each of the configured responder clients.
func generateCommentAddedAsyncEvent(ctx api.Context, event postq.Event) error {
	commentID := event.Properties["id"]

	var comment api.Comment
	err := ctx.DB().Where("id = ? AND external_id IS NULL", commentID).First(&comment).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			logger.Debugf("Skipping comment %s since it was added via responder", commentID)
			return nil
		}

		return err
	}

	// Get all responders related to a comment
	var responders []api.Responder
	commentRespondersQuery := `
		SELECT * FROM responders WHERE incident_id IN (
			SELECT incident_id FROM comments WHERE id = ?
		)
	`
	if err = ctx.DB().Raw(commentRespondersQuery, commentID).Preload("Team").Find(&responders).Error; err != nil {
		return err
	}

	for _, responder := range responders {
		switch responder.Type {
		case "jira":
			if err := ctx.DB().Clauses(eventQueueOnConflictClause).Create(&api.Event{Name: EventJiraCommentAdded, Properties: map[string]string{
				"responder_id": responder.ID.String(),
				"id":           commentID,
			}}).Error; err != nil {
				return err
			}
		case "ms_planner":
			if err := ctx.DB().Clauses(eventQueueOnConflictClause).Create(&api.Event{Name: EventMSPlannerCommentAdded, Properties: map[string]string{
				"responder_id": responder.ID.String(),
				"id":           commentID,
			}}).Error; err != nil {
				return err
			}
		}
	}

	return nil
}

func processResponderEvents(ctx api.Context, events postq.Events) postq.Events {
	var failedEvents []postq.Event
	for _, e := range events {
		if err := handleResponderEvent(ctx, e); err != nil {
			e.SetError(err.Error())
			failedEvents = append(failedEvents, e)
		}
	}

	return failedEvents
}

func handleResponderEvent(ctx api.Context, event postq.Event) error {
	switch event.Name {
	case EventJiraResponderAdded, EventMSPlannerResponderAdded:
		return reconcileResponderEvent(ctx, event)
	case EventJiraCommentAdded, EventMSPlannerCommentAdded:
		return reconcileCommentEvent(ctx, event)
	default:
		return fmt.Errorf("unrecognized event name: %s", event.Name)
	}
}

// TODO: Modify this such that it only notifies the responder mentioned in the event.
func reconcileResponderEvent(ctx api.Context, event postq.Event) error {
	responderID := event.Properties["id"]

	var responder api.Responder
	err := ctx.DB().Where("id = ? AND external_id is NULL", responderID).Preload("Incident").Preload("Team").Find(&responder).Error
	if err != nil {
		return err
	}

	responderClient, err := pkgResponder.GetResponder(ctx, responder.Team)
	if err != nil {
		return err
	}

	externalID, err := responderClient.NotifyResponder(ctx, responder)
	if err != nil {
		return err
	}
	if externalID != "" {
		// Update external id in responder table
		return ctx.DB().Model(&api.Responder{}).Where("id = ?", responder.ID).Update("external_id", externalID).Error
	}

	return nil
}

// TODO: Modify this such that it only adds the comment to the particular responder mentioned in the event.
func reconcileCommentEvent(ctx api.Context, event postq.Event) error {
	commentID := event.Properties["id"]

	var comment api.Comment
	err := ctx.DB().Where("id = ? AND external_id IS NULL", commentID).First(&comment).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			logger.Debugf("Skipping comment %s since it was added via responder", commentID)
			return nil
		}

		return err
	}

	// Get all responders related to a comment
	var responders []api.Responder
	commentRespondersQuery := `
        SELECT * FROM responders WHERE incident_id IN (
            SELECT incident_id FROM comments WHERE id = ?
        )
    `
	if err = ctx.DB().Raw(commentRespondersQuery, commentID).Preload("Team").Find(&responders).Error; err != nil {
		return err
	}

	// For each responder add the comment
	for _, _responder := range responders {
		// Reset externalID to avoid inserting previous iteration's ID
		externalID := ""

		responder, err := pkgResponder.GetResponder(ctx, _responder.Team)
		if err != nil {
			return err
		}

		externalID, err = responder.NotifyResponderAddComment(ctx, _responder, comment.Comment)
		if err != nil {
			// TODO: Associate error messages with responderType and handle specific responders when reprocessing
			logger.Errorf("error adding comment to responder:%s %v", _responder.Properties["responderType"], err)
			continue
		}

		// Insert into comment_responders table
		if externalID != "" {
			err = ctx.DB().Exec("INSERT INTO comment_responders (comment_id, responder_id, external_id) VALUES (?, ?, ?)",
				commentID, _responder.ID, externalID).Error
			if err != nil {
				logger.Errorf("error updating comment_responders table: %v", err)
			}
		}
	}

	return nil
}
