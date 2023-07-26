package db

import (
	"context"

	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
)

func GetCanariesOfAgent(ctx context.Context, agentID uuid.UUID) ([]models.Canary, error) {
	var canaries []models.Canary
	err := Gorm.WithContext(ctx).Where("agent_id = ?", agentID).Find(&canaries).Error
	return canaries, err
}
