package db

import (
	"errors"
	"fmt"

	"github.com/flanksource/commons/collections"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"gorm.io/gorm"
)

// CheckAgentsExist returns all the non existing agents from the given names.
func CheckAgentsExist(ctx context.Context, names ...string) ([]string, error) {
	var found []string
	err := ctx.DB().Model(&models.Agent{}).Select("name").
		Where("name IN ?", names).
		Find(&found).Error
	if err != nil {
		return nil, err
	}

	diff, _ := lo.Difference(names, found)
	return diff, nil
}

func FindAgent(ctx context.Context, name string) (*models.Agent, error) {
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

func GetAgent(ctx context.Context, name string) (*models.Agent, error) {
	var t models.Agent
	tx := ctx.DB().Where("name = ?", name).First(&t)
	return &t, tx.Error
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
