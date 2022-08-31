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
		selectedComps := teams.GetComponentsWithSelectors(compSelectors)
		teamComponents := teams.GetTeamComponents(teamID, selectedComps)
		teams.PersistTeamComponents(teamComponents)
	}
}
