package db

import (
	"errors"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func FindPlaybook(ctx *api.Context, id uuid.UUID) (*models.Playbook, error) {
	var p models.Playbook
	if err := ctx.DB().Where("id = ?", id).First(&p).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}

		return nil, err
	}

	return &p, nil
}

func FindPlaybookRun(ctx *api.Context, id string) (*models.PlaybookRun, error) {
	var p models.PlaybookRun
	if err := ctx.DB().Where("id = ?", id).First(&p).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}

		return nil, err
	}

	return &p, nil
}

func GetScheduledPlaybookRuns(ctx *api.Context, exceptions ...uuid.UUID) ([]models.PlaybookRun, error) {
	var runs []models.PlaybookRun
	if err := ctx.DB().Not(exceptions).Where("status = ?", models.PlaybookRunStatusScheduled).Find(&runs).Error; err != nil {
		return nil, err
	}

	return runs, nil
}
