package db

import (
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/incident-commander/api"
	"github.com/google/uuid"
	"github.com/patrickmn/go-cache"
	"gorm.io/gorm/clause"
)

var (
	componentSelectorCache = cache.New(cache.NoExpiration, cache.NoExpiration)

	componentSelectorMutableCache = cache.New(time.Minute*5, time.Minute*5)
)

func GetTeamsWithComponentSelector(ctx context.Context) map[uuid.UUID][]api.ComponentSelector {
	var teams []api.Team
	var teamComponentMap = make(map[uuid.UUID][]api.ComponentSelector)
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

func GetComponentsWithSelector(ctx context.Context, selector api.ComponentSelector) ([]uuid.UUID, error) {
	var cacheToUse = componentSelectorMutableCache
	if len(selector.Labels) == 0 && len(selector.Types) == 0 {
		cacheToUse = componentSelectorCache
	}

	if val, ok := cacheToUse.Get(selector.Hash()); ok {
		return val.([]uuid.UUID), nil
	}

	var compIds []uuid.UUID
	query := ctx.DB().Table("components").Where("deleted_at is null").Select("id")
	if selector.Name != "" {
		query = query.Where("name = ?", selector.Name)
	}
	if selector.Namespace != "" {
		query = query.Where("namespace = ?", selector.Namespace)
	}

	query = selector.Types.Where(query, "type")

	if selector.Labels != nil {
		query = query.Where("labels @> ?", selector.Labels)
	}

	if err := query.Find(&compIds).Error; err != nil {
		return nil, err
	}

	cacheToUse.SetDefault(selector.Hash(), compIds)

	return compIds, nil
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

func LookupRelatedComponentIDs(componentID string, maxDepth int) ([]string, error) {
	var componentIDs []string

	var childRows []struct {
		ChildID  string
		ParentID string
	}
	if err := Gorm.Raw(`SELECT child_id, parent_id FROM lookup_component_children(?, ?)`, componentID, maxDepth).
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
	if err := Gorm.Raw(`SELECT id FROM lookup_component_relations(?)`, componentID).
		Scan(&relatedRows).Error; err != nil {
		return componentIDs, err
	}
	componentIDs = append(componentIDs, relatedRows...)
	return componentIDs, nil
}

func LookupIncidentsByComponent(componentID string) ([]string, error) {
	var incidentIDs []string
	if err := Gorm.Raw(`SELECT id FROM lookup_component_incidents(?)`, componentID).
		Scan(&incidentIDs).Error; err != nil {
		return incidentIDs, err
	}

	return incidentIDs, nil
}

func LookupConfigsByComponent(componentID string) ([]string, error) {
	var configIDs []string
	err := Gorm.Raw(`SELECT config_id FROM config_component_relationships WHERE component_id = ?`, componentID).
		Scan(&configIDs).Error
	if err != nil {
		return configIDs, err
	}

	return configIDs, err
}
