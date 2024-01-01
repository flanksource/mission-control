package responder

import (
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/events"

	"github.com/flanksource/postq"
)

func init() {
	events.Register(RegisterEvents)
}

func RegisterEvents(ctx context.Context) {
	if ctx.Properties()[api.PropertyIncidentsDisabled] == "true" {
		return
	}
	events.RegisterSyncHandler(generateResponderAddedAsyncEvent, api.EventIncidentResponderAdded)
	events.RegisterSyncHandler(generateCommentAddedAsyncEvent, api.EventIncidentCommentAdded)
	events.RegisterAsyncHandler(processResponderEvents, 1, 5, api.EventJiraResponderAdded, api.EventMSPlannerResponderAdded, api.EventMSPlannerCommentAdded, api.EventJiraCommentAdded)
}

// generateResponderAddedAsyncEvent generates async events for each of the configured responder clients
// in the associated team.
func generateResponderAddedAsyncEvent(ctx context.Context, event postq.Event) error {
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
		if err := ctx.DB().Clauses(events.EventQueueOnConflictClause).Create(&api.Event{Name: api.EventJiraResponderAdded, Properties: map[string]string{"id": responderID}}).Error; err != nil {
			return err
		}
	}

	if spec.ResponderClients.MSPlanner != nil {
		if err := ctx.DB().Clauses(events.EventQueueOnConflictClause).Create(&api.Event{Name: api.EventMSPlannerResponderAdded, Properties: map[string]string{"id": responderID}}).Error; err != nil {
			return err
		}
	}

	return nil
}

// generateCommentAddedAsyncEvent generates comment.add async events for each of the configured responder clients.
func generateCommentAddedAsyncEvent(ctx context.Context, event postq.Event) error {
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
			if err := ctx.DB().Clauses(events.EventQueueOnConflictClause).Create(&api.Event{Name: api.EventJiraCommentAdded, Properties: map[string]string{
				"responder_id": responder.ID.String(),
				"id":           commentID,
			}}).Error; err != nil {
				return err
			}
		case "ms_planner":
			if err := ctx.DB().Clauses(events.EventQueueOnConflictClause).Create(&api.Event{Name: api.EventMSPlannerCommentAdded, Properties: map[string]string{
				"responder_id": responder.ID.String(),
				"id":           commentID,
			}}).Error; err != nil {
				return err
			}
		}
	}

	return nil
}

func processResponderEvents(ctx context.Context, events postq.Events) postq.Events {
	var failedEvents []postq.Event
	for _, e := range events {
		if err := handleResponderEvent(ctx, e); err != nil {
			e.SetError(err.Error())
			failedEvents = append(failedEvents, e)
		}
	}

	return failedEvents
}

func handleResponderEvent(ctx context.Context, event postq.Event) error {
	switch event.Name {
	case api.EventJiraResponderAdded, api.EventMSPlannerResponderAdded:
		return reconcileResponderEvent(ctx, event)
	case api.EventJiraCommentAdded, api.EventMSPlannerCommentAdded:
		return reconcileCommentEvent(ctx, event)
	default:
		return fmt.Errorf("unrecognized event name: %s", event.Name)
	}
}

// TODO: Modify this such that it only notifies the responder mentioned in the event.
func reconcileResponderEvent(ctx context.Context, event postq.Event) error {
	responderID := event.Properties["id"]

	var responder api.Responder
	err := ctx.DB().Where("id = ? AND external_id is NULL", responderID).Preload("Incident").Preload("Team").Find(&responder).Error
	if err != nil {
		return err
	}

	responderClient, err := GetResponder(ctx, responder.Team)
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
func reconcileCommentEvent(ctx context.Context, event postq.Event) error {
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

		responder, err := GetResponder(ctx, _responder.Team)
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
