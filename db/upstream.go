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

	if err := Gorm.WithContext(ctx).Not(req.ConfigScrapers).Find(&upstreamMsg.ConfigScrapers).Error; err != nil {
		return nil, fmt.Errorf("error fetching config scrapers: %w", err)
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

	var configScrapers []models.ConfigScraper
	if err := Gorm.Select("id").Where("agent_id = ?", agentID).Find(&configScrapers).Pluck("id", &response.ConfigScrapers).Error; err != nil {
		return nil, err
	}

	var configItems []models.ConfigItem
	if err := Gorm.Select("id").Where("agent_id = ?", agentID).Find(&configItems).Pluck("id", &response.ConfigItems).Error; err != nil {
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

	if len(req.ConfigScrapers) > 0 {
		if err := Gorm.Clauses(clause.OnConflict{UpdateAll: true}).Create(req.ConfigScrapers).Error; err != nil {
			return fmt.Errorf("error upserting config scrapers: %w", err)
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
