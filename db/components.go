package db

import (
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/incident-commander/api"
	"github.com/google/uuid"
	"gorm.io/gorm/clause"
)

func GetTeamsWithComponentSelector(ctx context.Context) map[uuid.UUID][]types.ResourceSelector {
	var teams []api.Team
	var teamComponentMap = make(map[uuid.UUID][]types.ResourceSelector)
	err := ctx.DB().Table("teams").Where("spec::jsonb ? 'components';").Find(&teams).Error
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

func PersistTeamComponents(ctx context.Context, teamComps []api.TeamComponent) error {
	if len(teamComps) == 0 {
		return nil
	}

	return ctx.DB().Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "team_id"}, {Name: "component_id"}, {Name: "selector_id"}},
		UpdateAll: true,
	}).Create(teamComps).Error
}

func LookupRelatedComponentIDs(ctx context.Context, componentID string, maxDepth int) ([]string, error) {
	var componentIDs []string

	var childRows []struct {
		ChildID  string
		ParentID string
	}
	if err := ctx.DB().Raw(`SELECT child_id, parent_id FROM lookup_component_children(?, ?)`, componentID, maxDepth).
		Scan(&childRows).Error; err != nil {
		return componentIDs, err
	}
	for _, row := range childRows {
		// The parent_id will be blank when the child is the root component
		if row.ParentID != "" {
			componentIDs = append(componentIDs, row.ParentID)
		}
		componentIDs = append(componentIDs, row.ChildID)
	}

	var relatedRows []string
	if err := ctx.DB().Raw(`SELECT id FROM lookup_component_relations(?)`, componentID).
		Scan(&relatedRows).Error; err != nil {
		return componentIDs, err
	}
	componentIDs = append(componentIDs, relatedRows...)
	return componentIDs, nil
}

func LookupIncidentsByComponent(ctx context.Context, componentID string) ([]string, error) {
	var incidentIDs []string
	if err := ctx.DB().Raw(`SELECT id FROM lookup_component_incidents(?)`, componentID).
		Scan(&incidentIDs).Error; err != nil {
		return incidentIDs, err
	}

	return incidentIDs, nil
}

func LookupConfigsByComponent(ctx context.Context, componentID string) ([]string, error) {
	var configIDs []string
	err := ctx.DB().Raw(`SELECT config_id FROM config_component_relationships WHERE component_id = ?`, componentID).
		Scan(&configIDs).Error
	if err != nil {
		return configIDs, err
	}

	return configIDs, err
}
