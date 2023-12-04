package db

import (
	"errors"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func FindArtifact(ctx context.Context, id uuid.UUID) (*models.Artifact, error) {
	var artifact models.Artifact
	if err := ctx.DB().Where("id = ?", id).First(&artifact).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}

		return nil, err
	}

	return &artifact, nil
}
