package jobs

import (
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/job"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/teams"
)

func TeamComponentOwnershipRun(ctx job.JobRuntime) error {
	logger.Debugf("Sync team components")
	teamComponentMap := db.GetTeamsWithComponentSelector(ctx.Context)
	for teamID, compSelectors := range teamComponentMap {
		teamComponents := teams.GetTeamComponentsFromSelectors(ctx.Context, teamID, compSelectors)
		err := db.PersistTeamComponents(ctx.Context, teamComponents)
		if err != nil {
			logger.Errorf("Error persisting team components: %v", err)
		}
	}
	return nil
}
