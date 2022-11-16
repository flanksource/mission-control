package jobs

import (
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/teams"
)

func TeamComponentOwnershipRun() {
	logger.Debugf("Sync team components")
	teamComponentMap := db.GetTeamsWithComponentSelector()
	for teamID, compSelectors := range teamComponentMap {
		teamComponents := teams.GetTeamComponentsFromSelectors(teamID, compSelectors)
		err := db.PersistTeamComponents(teamComponents)
		if err != nil {
			logger.Errorf("Error persisting team components: %v", err)
		}
	}
}
