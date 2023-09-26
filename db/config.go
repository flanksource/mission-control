package db

import (
	"fmt"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"github.com/google/uuid"
)

func LookupRelatedConfigIDs(configID string, maxDepth int) ([]string, error) {
	var configIDs []string

	var rows []struct {
		ChildID  string
		ParentID string
	}
	if err := Gorm.Raw(`SELECT child_id, parent_id FROM lookup_config_children(?, ?)`, configID, maxDepth).
		Scan(&rows).Error; err != nil {
		return configIDs, err
	}
	for _, row := range rows {
		configIDs = append(configIDs, row.ChildID, row.ParentID)
	}

	var relatedRows []string
	if err := Gorm.Raw(`SELECT id FROM lookup_config_relations(?)`, configID).
		Scan(&relatedRows).Error; err != nil {
		return configIDs, err
	}
	configIDs = append(configIDs, relatedRows...)

	return configIDs, nil
}

func GetScrapeConfigsOfAgent(ctx api.Context, agentID, since uuid.UUID) ([]models.ConfigScraper, error) {
	var response []models.ConfigScraper
	err := ctx.DB().Where("agent_id = ?", agentID).Where("id > ?", since).Order("id").Find(&response).Error
	return response, err
}

func GetLastPushedConfigResults(ctx api.Context, agentID uuid.UUID) (*api.LastPushedConfigResult, error) {
	var response api.LastPushedConfigResult

	if err := ctx.DB().Model(&models.ConfigItem{}).Where("agent_id = ?", agentID).Order("id DESC").Limit(1).Pluck("id", &response.ConfigID).Error; err != nil {
		return nil, fmt.Errorf("error getting last config item id: %w", err)
	}

	if err := ctx.DB().Model(&models.ConfigAnalysis{}).Where("agent_id = ?", agentID).Order("id DESC").Limit(1).Pluck("id", &response.AnalysisID).Error; err != nil {
		return nil, fmt.Errorf("error getting last analysis id: %w", err)
	}

	if err := ctx.DB().Model(&models.ConfigChange{}).Where("agent_id = ?", agentID).Order("id DESC").Limit(1).Pluck("id", &response.ChangeID).Error; err != nil {
		return nil, fmt.Errorf("error getting last change id: %w", err)
	}

	return &response, nil
}
