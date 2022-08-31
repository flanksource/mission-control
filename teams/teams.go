package teams

import (
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/utils"
	"github.com/google/uuid"
)

func PersistTeamComponents(teamComps []api.TeamComponent) {
	for _, teamComp := range teamComps {
		if err := db.PersistTeamComponent(teamComp); err != nil {
			logger.Errorf("error persisting team component")
		}
	}
}

func GetTeamComponents(teamId uuid.UUID, selectedComponents map[string][]uuid.UUID) []api.TeamComponent {
	var teamComps []api.TeamComponent
	for hash, selectedComps := range selectedComponents {
		for _, compID := range selectedComps {
			teamComps = append(teamComps,
				api.TeamComponent{
					TeamID:      teamId,
					SelectorID:  hash,
					ComponentID: compID,
				},
			)
		}
	}
	return teamComps
}

func GetComponentsWithSelectors(componentSelectors []api.ComponentSelector) map[string][]uuid.UUID {
	var selectedComponents = make(map[string][]uuid.UUID)
	for _, compSelector := range componentSelectors {
		selectedComponents[utils.GetHash(compSelector)] = db.GetComponentsWithSelector(compSelector)
	}
	return selectedComponents
}
