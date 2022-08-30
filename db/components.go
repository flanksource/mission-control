package db

import (
	"context"
	"encoding/json"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db/types"
	"github.com/flanksource/incident-commander/utils"
	"github.com/google/uuid"
	"gorm.io/gorm/clause"
)

func GetTeamsWithComponentSelector() map[uuid.UUID][]api.ComponentSelector {
	var teams []api.Team
	var teamComponentMap = make(map[uuid.UUID][]api.ComponentSelector)
	err := Gorm.Table("teams").Where("spec::jsonb ? 'components';").Find(&teams).Error
	if err != nil {
		logger.Errorf("error fetching the teams with componenets: %v", err)
	}
	for _, team := range teams {
		teamSpec := &api.TeamSpec{}
		teamSpecJson, err := team.Spec.MarshalJSON()
		if err != nil {
			logger.Errorf("error marshalling team spec for team: %v", team.ID)
			continue
		}
		if err := json.Unmarshal(teamSpecJson, teamSpec); err != nil {
			logger.Errorf("error unmarshalling the teamSpec for team: %v", team.ID)
		}
		teamComponentMap[team.ID] = teamSpec.Components
	}
	return teamComponentMap
}

func GetComponentsWithSelectors(componentSelectors []api.ComponentSelector) map[string][]uuid.UUID {
	var selectedComponents = make(map[string][]uuid.UUID)
	for _, compSelector := range componentSelectors {
		selectedComponents[utils.GetHash(compSelector)] = getComponentsWithSelector(compSelector)
	}

	return selectedComponents
}

func getComponentsWithSelector(selector api.ComponentSelector) []uuid.UUID {
	sql := "select ID from components where deleted_at is null "
	var compIds []uuid.UUID
	args := make(map[string]interface{})
	if selector.Name != "" {
		sql += " AND name = :name"
		args["name"] = selector.Name
	}

	if selector.Namespace != "" {
		sql += " AND namespace = :namespace"
		args["namespace"] = selector.Namespace
	}

	if selector.Type != "" {
		sql += " AND type = :type"
		args["type"] = selector.Type
	}

	if selector.Labels != nil {
		sql += "AND labels @> :labels"
		args["labels"] = types.JSONStringMap(selector.Labels)
	}
	rows, err := QueryNamed(context.Background(), sql, args)
	if err != nil {
		logger.Errorf("error fetching component with selector: %v", selector)
	}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			logger.Errorf("error scanning the id for the component")
			continue
		}
		compIds = append(compIds, id)
	}
	return compIds
}

func PersistTeamComponents(teamComps []api.TeamComponent) {
	for _, teamComp := range teamComps {
		if err := PersistTeamComponent(teamComp); err != nil {
			logger.Errorf("error persisting team component")
		}
	}
}

func PersistTeamComponent(teamComp api.TeamComponent) error {
	tx := Gorm.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "team_id"}, {Name: "component_id"}, {Name: "selector_id"}},
		UpdateAll: true,
	}).Create(teamComp)
	return tx.Error

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
