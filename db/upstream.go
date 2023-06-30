package db

import (
	"fmt"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"gorm.io/gorm/clause"
)

func GetAllMissingResourceIDs(ctx *api.Context, req *api.PushedResourceIDs) (*api.PushData, error) {
	var upstreamMsg api.PushData

	if err := ctx.DB().Not(req.Components).Find(&upstreamMsg.Components).Error; err != nil {
		return nil, fmt.Errorf("error fetching components: %w", err)
	}

	if err := ctx.DB().Not(req.ConfigScrapers).Find(&upstreamMsg.ConfigScrapers).Error; err != nil {
		return nil, fmt.Errorf("error fetching config scrapers: %w", err)
	}

	if err := ctx.DB().Not(req.ConfigItems).Find(&upstreamMsg.ConfigItems).Error; err != nil {
		return nil, fmt.Errorf("error fetching config items: %w", err)
	}

	if err := ctx.DB().Not(req.Canaries).Find(&upstreamMsg.Canaries).Error; err != nil {
		return nil, fmt.Errorf("error fetching canaries: %w", err)
	}

	if err := ctx.DB().Not(req.Checks).Find(&upstreamMsg.Checks).Error; err != nil {
		return nil, fmt.Errorf("error fetching checks: %w", err)
	}

	return &upstreamMsg, nil
}

func GetAllResourceIDsOfAgent(ctx *api.Context, agentID string) (*api.PushedResourceIDs, error) {
	var response api.PushedResourceIDs

	var canaries []models.Canary
	if err := ctx.DB().Select("id").Where("agent_id = ?", agentID).Find(&canaries).Pluck("id", &response.Canaries).Error; err != nil {
		return nil, err
	}

	var checks []models.Check
	if err := ctx.DB().Select("id").Where("agent_id = ?", agentID).Find(&checks).Pluck("id", &response.Checks).Error; err != nil {
		return nil, err
	}

	var components []models.Component
	if err := ctx.DB().Select("id").Where("agent_id = ?", agentID).Find(&components).Pluck("id", &response.Components).Error; err != nil {
		return nil, err
	}

	var configScrapers []models.ConfigScraper
	if err := ctx.DB().Select("id").Where("agent_id = ?", agentID).Find(&configScrapers).Pluck("id", &response.ConfigScrapers).Error; err != nil {
		return nil, err
	}

	var configItems []models.ConfigItem
	if err := ctx.DB().Select("id").Where("agent_id = ?", agentID).Find(&configItems).Pluck("id", &response.ConfigItems).Error; err != nil {
		return nil, err
	}

	return &response, nil
}

func InsertUpstreamMsg(ctx *api.Context, req *api.PushData) error {
	if len(req.Canaries) > 0 {
		if err := ctx.DB().Clauses(clause.OnConflict{UpdateAll: true}).Create(req.Canaries).Error; err != nil {
			return fmt.Errorf("error upserting canaries: %w", err)
		}
	}

	// components are inserted one by one, instead of in a batch, because of the foreign key constraint with itself.
	for _, c := range req.Components {
		if err := ctx.DB().Clauses(clause.OnConflict{UpdateAll: true}).Create(&c).Error; err != nil {
			logger.Errorf("error upserting component (id=%s): %w", c.ID, err)
		}
	}

	if len(req.ComponentRelationships) > 0 {
		cols := []clause.Column{{Name: "component_id"}, {Name: "relationship_id"}, {Name: "selector_id"}}
		if err := ctx.DB().Clauses(clause.OnConflict{UpdateAll: true, Columns: cols}).Create(req.ComponentRelationships).Error; err != nil {
			return fmt.Errorf("error upserting component_relationships: %w", err)
		}
	}

	if len(req.ConfigScrapers) > 0 {
		if err := ctx.DB().Clauses(clause.OnConflict{UpdateAll: true}).Create(req.ConfigScrapers).Error; err != nil {
			return fmt.Errorf("error upserting config scrapers: %w", err)
		}
	}

	// config items are inserted one by one, instead of in a batch, because of the foreign key constraint with itself.
	for _, ci := range req.ConfigItems {
		if err := ctx.DB().Clauses(clause.OnConflict{UpdateAll: true}).Create(&ci).Error; err != nil {
			logger.Errorf("error upserting config item (id=%s): %w", ci.ID, err)
		}
	}

	if len(req.ConfigRelationships) > 0 {
		cols := []clause.Column{{Name: "related_id"}, {Name: "config_id"}, {Name: "selector_id"}}
		if err := ctx.DB().Clauses(clause.OnConflict{UpdateAll: true, Columns: cols}).Create(req.ConfigRelationships).Error; err != nil {
			return fmt.Errorf("error upserting config_relationships: %w", err)
		}
	}

	if len(req.ConfigComponentRelationships) > 0 {
		cols := []clause.Column{{Name: "component_id"}, {Name: "config_id"}}
		if err := ctx.DB().Clauses(clause.OnConflict{UpdateAll: true, Columns: cols}).Create(req.ConfigComponentRelationships).Error; err != nil {
			return fmt.Errorf("error upserting config_component_relationships: %w", err)
		}
	}

	if len(req.ConfigChanges) > 0 {
		if err := ctx.DB().Clauses(clause.OnConflict{UpdateAll: true}).Create(req.ConfigChanges).Error; err != nil {
			return fmt.Errorf("error upserting config_changes: %w", err)
		}
	}

	if len(req.ConfigAnalysis) > 0 {
		if err := ctx.DB().Clauses(clause.OnConflict{UpdateAll: true}).Create(req.ConfigAnalysis).Error; err != nil {
			return fmt.Errorf("error upserting config_analysis: %w", err)
		}
	}

	if len(req.Checks) > 0 {
		if err := ctx.DB().Clauses(clause.OnConflict{UpdateAll: true}).Create(req.Checks).Error; err != nil {
			return fmt.Errorf("error upserting checks: %w", err)
		}
	}

	if len(req.CheckStatuses) > 0 {
		cols := []clause.Column{{Name: "check_id"}, {Name: "time"}}
		if err := ctx.DB().Clauses(clause.OnConflict{UpdateAll: true, Columns: cols}).Create(req.CheckStatuses).Error; err != nil {
			return fmt.Errorf("error upserting check_statuses: %w", err)
		}
	}

	return nil
}
