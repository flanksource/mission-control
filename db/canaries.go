package db

import (
	"context"
	"time"

	"github.com/flanksource/incident-commander/api"
	"github.com/google/uuid"
)

func GetCanariesOfAgent(ctx context.Context, agentID uuid.UUID, since time.Time) (*api.CanaryPullResponse, error) {
	var now time.Time
	if err := Gorm.WithContext(ctx).Raw("SELECT NOW()").Scan(&now).Error; err != nil {
		return nil, err
	}

	q := Gorm.WithContext(ctx).Where("agent_id = ?", agentID).Where("updated_at <= ?", now)
	if !since.IsZero() {
		q = q.Where("updated_at > ?", since)
	}

	response := &api.CanaryPullResponse{
		Before: now,
	}
	err := q.Find(&response.Canaries).Error
	return response, err
}
