package db

import (
	"context"
	"fmt"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"gorm.io/gorm/clause"
)

// TODO: Is it ncessesary to check for agent_id IS NULL here?
// At this point we're sure that it's an agent so the agent_id is always going to be nil UUID.
// Unless this is both an agent and an upstream ... ?
func GetAllMissingResourceIDs(ctx context.Context, req *api.PushedResourceIDs) (*api.PushData, error) {
	var upstreamMsg api.PushData

	if err := Gorm.WithContext(ctx).Not(req.Components).Find(&upstreamMsg.Components).Error; err != nil {
		return nil, fmt.Errorf("error fetching components: %w", err)
	}

	if err := Gorm.WithContext(ctx).Not(req.ConfigItems).Find(&upstreamMsg.ConfigItems).Error; err != nil {
		return nil, fmt.Errorf("error fetching config items: %w", err)
	}

	if err := Gorm.WithContext(ctx).Not(req.Canaries).Find(&upstreamMsg.Canaries).Error; err != nil {
		return nil, fmt.Errorf("error fetching canaries: %w", err)
	}

	if err := Gorm.WithContext(ctx).Not(req.Checks).Find(&upstreamMsg.Checks).Error; err != nil {
		return nil, fmt.Errorf("error fetching checks: %w", err)
	}

	if err := Gorm.WithContext(ctx).Not(req.ConfigAnalysis).Find(&upstreamMsg.ConfigAnalysis).Error; err != nil {
		return nil, fmt.Errorf("error fetching config analyses: %w", err)
	}

	if err := Gorm.WithContext(ctx).Not(req.ConfigChanges).Find(&upstreamMsg.ConfigChanges).Error; err != nil {
		return nil, fmt.Errorf("error fetching config changes: %w", err)
	}

	checkStatusQuery := Gorm.WithContext(ctx)
	for _, cs := range req.CheckStatuses {
		checkStatusQuery = checkStatusQuery.Not("check_id = ? AND time = ?", cs.CheckID, cs.Time)
	}
	if err := checkStatusQuery.Find(&upstreamMsg.CheckStatuses).Error; err != nil {
		return nil, fmt.Errorf("error fetching check statuses: %w", err)
	}

	ConfigComponentRelationshipQuery := Gorm.WithContext(ctx)
	for _, cs := range req.ConfigComponentRelationships {
		ConfigComponentRelationshipQuery = ConfigComponentRelationshipQuery.Not("component_id = ? AND config_id = ?", cs.ComponentID, cs.ConfigID)
	}
	if err := ConfigComponentRelationshipQuery.Find(&upstreamMsg.ConfigComponentRelationships).Error; err != nil {
		return nil, fmt.Errorf("error fetching config component relationship: %w", err)
	}

	componentRelationshipQuery := Gorm.WithContext(ctx)
	for _, cs := range req.ComponentRelationships {
		componentRelationshipQuery = componentRelationshipQuery.Not("component_id = ? AND relationship_id = ? AND selector_id = ?", cs.ComponentID, cs.RelationshipID, cs.SelectorID)
	}
	if err := componentRelationshipQuery.Find(&upstreamMsg.ComponentRelationships).Error; err != nil {
		return nil, fmt.Errorf("error fetching components relationships: %w", err)
	}

	configRelationshipQuery := Gorm.WithContext(ctx)
	for _, cs := range req.ConfigRelationships {
		configRelationshipQuery = configRelationshipQuery.Not("related_id = ? AND config_id = ? AND selector_id = ?", cs.RelatedID, cs.ConfigID, cs.SelectorID)
	}
	if err := configRelationshipQuery.Find(&upstreamMsg.ConfigRelationships).Error; err != nil {
		return nil, fmt.Errorf("error fetching config relationships: %w", err)
	}

	return &upstreamMsg, nil
}

func GetAllResourceIDsOfAgent(ctx context.Context, agentID string) (*api.PushedResourceIDs, error) {
	var response api.PushedResourceIDs

	var canaries []models.Canary
	if err := Gorm.Select("id").Where("agent_id = ?", agentID).Find(&canaries).Pluck("id", &response.Canaries).Error; err != nil {
		return nil, err
	}

	var checks []models.Check
	if err := Gorm.Select("id").Where("agent_id = ?", agentID).Find(&checks).Pluck("id", &response.Checks).Error; err != nil {
		return nil, err
	}

	var components []models.Component
	if err := Gorm.Select("id").Where("agent_id = ?", agentID).Find(&components).Pluck("id", &response.Components).Error; err != nil {
		return nil, err
	}

	var configItems []models.ConfigItem
	if err := Gorm.Select("id").Where("agent_id = ?", agentID).Find(&configItems).Pluck("id", &response.ConfigItems).Error; err != nil {
		return nil, err
	}

	var configAnalysis []models.ConfigAnalysis
	if err := Gorm.Select("id").Where("config_id IN (?)", response.ConfigItems).Find(&configAnalysis).Pluck("id", &response.ConfigAnalysis).Error; err != nil {
		return nil, err
	}

	var configChanges []models.ConfigChange
	if err := Gorm.Select("id").Where("config_id IN (?)", response.ConfigItems).Find(&configChanges).Pluck("id", &response.ConfigChanges).Error; err != nil {
		return nil, err
	}

	if err := Gorm.Select("check_id, time").Where("check_id IN (?)", response.Checks).Find(&response.CheckStatuses).Error; err != nil {
		return nil, err
	}

	if err := Gorm.Select("config_id, component_id").Where("config_id IN (?) OR component_id IN (?)", response.ConfigItems, response.Components).Find(&response.ConfigComponentRelationships).Error; err != nil {
		return nil, err
	}

	if err := Gorm.Select("related_id, config_id, selector_id").Where("config_id IN (?)", response.ConfigItems).Find(&response.ConfigRelationships).Error; err != nil {
		return nil, err
	}

	if err := Gorm.Select("component_id, relationship_id, selector_id").Where("component_id IN (?)", response.Components).Find(&response.ComponentRelationships).Error; err != nil {
		return nil, err
	}

	return &response, nil
}

func InsertUpstreamMsg(ctx context.Context, req *api.PushData) error {
	if len(req.Canaries) > 0 {
		if err := Gorm.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(req.Canaries, 500).Error; err != nil {
			return fmt.Errorf("error upserting canaries: %w", err)
		}
	}

	if len(req.Components) > 0 {
		if err := Gorm.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(req.Components, 500).Error; err != nil {
			return fmt.Errorf("error upserting components: %w", err)
		}
	}

	if len(req.ComponentRelationships) > 0 {
		cols := []clause.Column{{Name: "component_id"}, {Name: "relationship_id"}, {Name: "selector_id"}}
		if err := Gorm.Clauses(clause.OnConflict{UpdateAll: true, Columns: cols}).CreateInBatches(req.ComponentRelationships, 500).Error; err != nil {
			return fmt.Errorf("error upserting component_relationships: %w", err)
		}
	}

	if len(req.ConfigItems) > 0 {
		if err := Gorm.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(req.ConfigItems, 500).Error; err != nil {
			return fmt.Errorf("error upserting config_items: %w", err)
		}
	}

	if len(req.ConfigRelationships) > 0 {
		cols := []clause.Column{{Name: "related_id"}, {Name: "config_id"}, {Name: "selector_id"}}
		if err := Gorm.Clauses(clause.OnConflict{UpdateAll: true, Columns: cols}).CreateInBatches(req.ConfigRelationships, 500).Error; err != nil {
			return fmt.Errorf("error upserting config_relationships: %w", err)
		}
	}

	if len(req.ConfigComponentRelationships) > 0 {
		cols := []clause.Column{{Name: "component_id"}, {Name: "config_id"}}
		if err := Gorm.Clauses(clause.OnConflict{UpdateAll: true, Columns: cols}).CreateInBatches(req.ConfigComponentRelationships, 500).Error; err != nil {
			return fmt.Errorf("error upserting config_component_relationships: %w", err)
		}
	}

	if len(req.ConfigChanges) > 0 {
		if err := Gorm.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(req.ConfigChanges, 500).Error; err != nil {
			return fmt.Errorf("error upserting config_changes: %w", err)
		}
	}

	if len(req.ConfigAnalysis) > 0 {
		if err := Gorm.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(req.ConfigAnalysis, 500).Error; err != nil {
			return fmt.Errorf("error upserting config_analysis: %w", err)
		}
	}

	if len(req.Checks) > 0 {
		if err := Gorm.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(req.Checks, 500).Error; err != nil {
			return fmt.Errorf("error upserting checks: %w", err)
		}
	}

	if len(req.CheckStatuses) > 0 {
		cols := []clause.Column{{Name: "check_id"}, {Name: "time"}}
		if err := Gorm.Clauses(clause.OnConflict{UpdateAll: true, Columns: cols}).CreateInBatches(req.CheckStatuses, 500).Error; err != nil {
			return fmt.Errorf("error upserting check_statuses: %w", err)
		}
	}

	return nil
}
