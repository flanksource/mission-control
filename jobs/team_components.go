package jobs

import (
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/db"
)

func TeamComponentOwnershipRun() {
	logger.Debugf("Sync team components")
	teamComponentMap := db.GetTeamsWithComponentSelector()
	for teamID, compSelectors := range teamComponentMap {
		selectedComps := db.GetComponentsWithSelectors(compSelectors)
		teamComponents := db.GetTeamComponents(teamID, selectedComps)
		db.PersistTeamComponents(teamComponents)
	}
}
