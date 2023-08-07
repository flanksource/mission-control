package db

import (
	"errors"
	"fmt"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func FindAgent(ctx *api.Context, name string) (*models.Agent, error) {
	var agent models.Agent
	err := ctx.DB().Where("name = ?", name).First(&agent).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}

		return nil, err
	}

	return &agent, nil
}

func getAgent(ctx *api.Context, name string) (*models.Agent, error) {
	var t models.Agent
	tx := ctx.DB().Where("name = ?", name).First(&t)
	return &t, tx.Error
}

func createAgent(ctx *api.Context, name string) (*models.Agent, error) {
	a := models.Agent{Name: name}
	tx := ctx.DB().Create(&a)
	return &a, tx.Error
}

func GetOrCreateAgent(ctx *api.Context, name string) (*models.Agent, error) {
	a, err := getAgent(ctx, name)
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

func CreateAgent(ctx *api.Context, name string, personID *uuid.UUID, properties map[string]string) error {
	a := models.Agent{
		Name:       name,
		PersonID:   personID,
		Properties: properties,
	}

	return ctx.DB().Create(&a).Error
}
