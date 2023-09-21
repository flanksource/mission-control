package db

import (
	"time"

	"github.com/flanksource/incident-commander/api"
	"github.com/google/uuid"
)

func GetCanariesOfAgent(ctx api.Context, agentID uuid.UUID, since time.Time) (*api.CanaryPullResponse, error) {
	var now time.Time
	if err := ctx.DB().Raw("SELECT NOW()").Scan(&now).Error; err != nil {
		return nil, err
	}

	q := ctx.DB().Where("agent_id = ?", agentID).Where("updated_at <= ?", now)
	if !since.IsZero() {
		q = q.Where("updated_at > ?", since)
	}

	response := &api.CanaryPullResponse{
		Before: now,
	}
	err := q.Find(&response.Canaries).Error
	return response, err
}
