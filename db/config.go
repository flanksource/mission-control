package db

import (
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
