package teams

import (
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/utils"
	"github.com/google/uuid"
)

func GetTeamComponentsFromSelectors(teamID uuid.UUID, componentSelectors []api.ComponentSelector) []api.TeamComponent {
	var selectedComponents = make(map[string][]uuid.UUID)
	for _, compSelector := range componentSelectors {
		selectedComponents[utils.GetHash(compSelector)] = db.GetComponentsWithSelector(compSelector)
	}

	var teamComps []api.TeamComponent
	for hash, selectedComps := range selectedComponents {
		for _, compID := range selectedComps {
			teamComps = append(teamComps,
				api.TeamComponent{
					TeamID:      teamID,
					SelectorID:  hash,
					ComponentID: compID,
				},
			)
		}
	}
	return teamComps
}
