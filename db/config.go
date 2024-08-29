package db

import (
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
)

func LookupRelatedConfigIDs(ctx context.Context, configID string, maxDepth int) ([]string, error) {
	var configIDs []string

	var rows []struct {
		ChildID  string
		ParentID string
	}
	if err := ctx.DB().Raw(`SELECT child_id, parent_id FROM lookup_config_children(?, ?)`, configID, maxDepth).
		Scan(&rows).Error; err != nil {
		return configIDs, err
	}
	for _, row := range rows {
		configIDs = append(configIDs, row.ChildID, row.ParentID)
	}

	var relatedRows []string
	if err := ctx.DB().Raw(`SELECT id FROM lookup_config_relations(?)`, configID).
		Scan(&relatedRows).Error; err != nil {
		return configIDs, err
	}
	configIDs = append(configIDs, relatedRows...)

	return configIDs, nil
}

func GetScrapeConfigsOfAgent(ctx context.Context, agentID uuid.UUID, since time.Time) ([]models.ConfigScraper, error) {
	var response []models.ConfigScraper
	err := ctx.DB().Where("agent_id = ?", agentID).Where("updated_at > ?", since).Order("updated_at").Find(&response).Error
	return response, err
}
