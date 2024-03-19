package db

import (
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
)

func GetProperties(ctx context.Context) ([]models.AppProperty, error) {
	var properties []models.AppProperty
	err := ctx.DB().Find(&properties).Error
	return properties, err
}
