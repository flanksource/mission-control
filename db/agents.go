package db

import (
	"errors"
	"fmt"

	"github.com/flanksource/commons/collections"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func FindFirstAgent(ctx context.Context, names ...string) (*models.Agent, error) {
	for _, name := range names {
		agent, err := FindAgent(ctx, name)
		if err != nil {
			return nil, err
		}
		if agent != nil {
			return agent, nil
		}
	}
	return nil, nil
}

func FindAgent(ctx context.Context, name string) (*models.Agent, error) {
	var t []models.Agent
	if id, err := uuid.Parse(name); err == nil {
		if err := ctx.DB().Where("id = ?", id).Find(&t).Error; err != nil {
			return nil, err
		}
		if len(t) > 0 {
			return &t[0], nil
		}
	}
	err := ctx.DB().Where("name = ?", name).Find(&t).Error
	if len(t) > 0 {
		return &t[0], nil
	}
	return nil, err
}

// Deprecated used FindAgent
func GetAgent(ctx context.Context, name string) (*models.Agent, error) {
	return FindAgent(ctx, name)
}

func createAgent(ctx context.Context, name string) (*models.Agent, error) {
	a := models.Agent{Name: name}
	tx := ctx.DB().Create(&a)
	return &a, tx.Error
}

func GetOrCreateAgent(ctx context.Context, name string) (*models.Agent, error) {
	a, err := GetAgent(ctx, name)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			newAgent, err := createAgent(ctx, name)
			if err != nil {
				return nil, fmt.Errorf("failed to create agent: %w", err)
			}
			return newAgent, nil
		}
		return nil, err
	}

	return a, nil
}

func CreateAgent(ctx context.Context, name string, personID *uuid.UUID, properties map[string]string) error {
	properties = collections.MergeMap(properties, map[string]string{"type": "agent"})

	a := models.Agent{
		Name:       name,
		PersonID:   personID,
		Properties: properties,
	}

	return ctx.DB().Create(&a).Error
}
