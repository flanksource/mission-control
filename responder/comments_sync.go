package responder

import (
	"database/sql"

	"github.com/flanksource/commons/collections"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/job"
	"github.com/google/uuid"

	"github.com/flanksource/incident-commander/api"
)

func getRootHypothesisOfIncident(ctx context.Context, incidentID uuid.UUID) (api.Hypothesis, error) {
	var hypothesis api.Hypothesis
	if err := ctx.DB().Where("incident_id = ? AND type = ?", incidentID, "root").First(&hypothesis).Error; err != nil {
		return hypothesis, err
	}
	return hypothesis, nil
}

func SyncComments(ctx job.JobRuntime) error {
	var responders []api.Responder
	err := ctx.DB().Where("external_id IS NOT NULL").Preload("Team").Find(&responders).Error
	if err != nil {
		return err
	}

	dbSelectExternalIDQuery := `
        SELECT external_id FROM comments WHERE responder_id = @responder_id
        UNION
        SELECT external_id FROM comment_responders WHERE responder_id = @responder_id
    `

	for _, responder := range responders {
		if !responder.Team.HasResponder() {
			logger.Debugf("Skipping responder %s since it does not have a responder", responder.Team.Name)
			continue
		}

		responderClient, err := GetResponder(ctx.Context, responder.Team)
		if err != nil {
			ctx.History.AddError(err.Error())
			continue
		}

		comments, err := responderClient.GetComments(responder.ExternalID)
		if err != nil {
			ctx.History.AddError(err.Error())
			continue
		}

		// Query all external_ids from comments and comment_responders table
		var dbExternalIDs []string
		err = ctx.Context.DB().Raw(dbSelectExternalIDQuery, sql.Named("responder_id", responder.ID)).Find(&dbExternalIDs).Error
		if err != nil {
			ctx.History.AddError(err.Error())
			continue
		}

		// IDs which are in Jira but not added to database must be added in the comments table
		for _, responderComment := range comments {
			if !collections.Contains(dbExternalIDs, responderComment.ExternalID) {
				rootHypothesis, err := getRootHypothesisOfIncident(ctx.Context, responder.IncidentID)
				if err != nil {
					logger.Errorf("Error fetching hypothesis from database: %v", err)
					continue
				}
				responderComment.IncidentID = responder.IncidentID
				responderComment.CreatedBy = *api.SystemUserID
				responderComment.ResponderID = &responder.ID
				responderComment.HypothesisID = &rootHypothesis.ID

				err = ctx.Context.DB().Create(&responderComment).Error
				if err != nil {
					ctx.History.AddError(err.Error())
					continue
				}
				ctx.History.IncrSuccess()
			}
		}
	}

	return nil
}
