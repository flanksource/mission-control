package db

import (
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/utils"
	"github.com/google/uuid"
	"gorm.io/gorm/clause"
)

func GetTeamsWithComponentSelector() map[uuid.UUID][]api.ComponentSelector {
	var teams []api.Team
	var teamComponentMap = make(map[uuid.UUID][]api.ComponentSelector)
	err := Gorm.Table("teams").Where("spec::jsonb ? 'components';").Find(&teams).Error
	if err != nil {
		logger.Errorf("error fetching the teams with components: %v", err)
		return teamComponentMap
	}
	for _, team := range teams {
		teamSpec, err := team.GetSpec()
		if err != nil {
			logger.Errorf("error fetching teamSpec: %v", err)
			continue
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
	var compIds []uuid.UUID
	query := Gorm.Table("components").Where("deleted_at is null").Select("id")
	if selector.Name != "" {
		query = query.Where("name = ?", selector.Name)
	}
	if selector.Namespace != "" {
		query = query.Where("namespace = ?", selector.Namespace)
	}
	if selector.Type != "" {
		query = query.Where("type = ?", selector.Type)
	}
	if selector.Labels != nil {
		query = query.Where("labels @> ?", selector.Labels)
	}
	query.Find(&compIds)
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
	return Gorm.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "team_id"}, {Name: "component_id"}, {Name: "selector_id"}},
		UpdateAll: true,
	}).Create(teamComp).Error
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
