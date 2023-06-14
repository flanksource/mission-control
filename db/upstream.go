package db

import (
	"context"
	"fmt"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"gorm.io/gorm/clause"
)

func GetAllResourceIDsOfAgent(ctx context.Context, agentID string) (*api.IDsResponse, error) {
	var response api.IDsResponse

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
		if err := Gorm.Clauses(clause.OnConflict{UpdateAll: true}).Create(req.Canaries).Error; err != nil {
			return fmt.Errorf("error upserting canaries: %w", err)
		}
	}

	if len(req.Components) > 0 {
		if err := Gorm.Clauses(clause.OnConflict{UpdateAll: true}).Create(req.Components).Error; err != nil {
			return fmt.Errorf("error upserting components: %w", err)
		}
	}

	if len(req.ComponentRelationships) > 0 {
		cols := []clause.Column{{Name: "component_id"}, {Name: "relationship_id"}, {Name: "selector_id"}}
		if err := Gorm.Clauses(clause.OnConflict{UpdateAll: true, Columns: cols}).Create(req.ComponentRelationships).Error; err != nil {
			return fmt.Errorf("error upserting component_relationships: %w", err)
		}
	}

	if len(req.ConfigItems) > 0 {
		if err := Gorm.Clauses(clause.OnConflict{UpdateAll: true}).Create(req.ConfigItems).Error; err != nil {
			return fmt.Errorf("error upserting config_items: %w", err)
		}
	}

	if len(req.ConfigRelationships) > 0 {
		cols := []clause.Column{{Name: "related_id"}, {Name: "config_id"}, {Name: "selector_id"}}
		if err := Gorm.Clauses(clause.OnConflict{UpdateAll: true, Columns: cols}).Create(req.ConfigRelationships).Error; err != nil {
			return fmt.Errorf("error upserting config_relationships: %w", err)
		}
	}

	if len(req.ConfigComponentRelationships) > 0 {
		cols := []clause.Column{{Name: "component_id"}, {Name: "config_id"}}
		if err := Gorm.Clauses(clause.OnConflict{UpdateAll: true, Columns: cols}).Create(req.ConfigComponentRelationships).Error; err != nil {
			return fmt.Errorf("error upserting config_component_relationships: %w", err)
		}
	}

	if len(req.ConfigChanges) > 0 {
		if err := Gorm.Clauses(clause.OnConflict{UpdateAll: true}).Create(req.ConfigChanges).Error; err != nil {
			return fmt.Errorf("error upserting config_changes: %w", err)
		}
	}

	if len(req.ConfigAnalysis) > 0 {
		if err := Gorm.Clauses(clause.OnConflict{UpdateAll: true}).Create(req.ConfigAnalysis).Error; err != nil {
			return fmt.Errorf("error upserting config_analysis: %w", err)
		}
	}

	if len(req.Checks) > 0 {
		if err := Gorm.Clauses(clause.OnConflict{UpdateAll: true}).Create(req.Checks).Error; err != nil {
			return fmt.Errorf("error upserting checks: %w", err)
		}
	}

	if len(req.CheckStatuses) > 0 {
		cols := []clause.Column{{Name: "check_id"}, {Name: "time"}}
		if err := Gorm.Clauses(clause.OnConflict{UpdateAll: true, Columns: cols}).Create(req.CheckStatuses).Error; err != nil {
			return fmt.Errorf("error upserting check_statuses: %w", err)
		}
	}

	return nil
}
