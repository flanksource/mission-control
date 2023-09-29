package jobs

import (
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/teams"
)

func TeamComponentOwnershipRun(ctx api.Context) error {
	logger.Debugf("Sync team components")
	teamComponentMap := db.GetTeamsWithComponentSelector(ctx)
	for teamID, compSelectors := range teamComponentMap {
		teamComponents := teams.GetTeamComponentsFromSelectors(ctx, teamID, compSelectors)
		err := db.PersistTeamComponents(ctx, teamComponents)
		if err != nil {
			logger.Errorf("Error persisting team components: %v", err)
		}
	}
	return nil
}
