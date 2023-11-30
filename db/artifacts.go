package db

import (
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
)

func FindArtifact(ctx context.Context, id uuid.UUID) (*models.Artifact, error) {
	var artifact models.Artifact
	err := ctx.DB().Where("id = ?", id).Find(&artifact).Error
	return &artifact, err
}
