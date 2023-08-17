package db

import (
	"encoding/json"
	"errors"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
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

func PersistPlaybookFromCRD(obj *v1.Playbook) error {
	specJSON, err := json.Marshal(obj.Spec)
	if err != nil {
		return err
	}

	dbObj := models.Playbook{
		ID:        uuid.MustParse(string(obj.GetUID())),
		Name:      obj.Name,
		Spec:      specJSON,
		Source:    models.SourceCRD,
		CreatedBy: api.SystemUserID,
	}

	return Gorm.Save(&dbObj).Error
}

func DeletePlaybook(id string) error {
	return Gorm.Delete(&models.Playbook{}, "id = ?", id).Error
}
