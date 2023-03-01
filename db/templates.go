package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// headlessTemplateNamespace is a shared template namespace for
// downstream instances.
const headlessTemplateNamespace = "push"

func getHeadlessTemplate(ctx context.Context, name string) (*models.SystemTemplate, error) {
	template := models.SystemTemplate{Name: name, Namespace: headlessTemplateNamespace}
	tx := Gorm.WithContext(ctx).Where(template).First(&template)
	return &template, tx.Error
}

func createHeadlessTemplate(ctx context.Context, name string) (*models.SystemTemplate, error) {
	template := models.SystemTemplate{ID: uuid.New(), Name: name, Namespace: headlessTemplateNamespace}
	tx := Gorm.WithContext(ctx).Create(&template)
	return &template, tx.Error
}

func GetOrCreateHeadlessTemplateID(ctx context.Context, name string) (*models.SystemTemplate, error) {
	id, err := getHeadlessTemplate(ctx, name)
	if nil == err {
		return id, nil
	}

	if errors.Is(err, gorm.ErrRecordNotFound) {
		newHeadlessTpl, err := createHeadlessTemplate(ctx, name)
		if nil == err {
			return newHeadlessTpl, nil
		}

		if err != nil {
			return nil, fmt.Errorf("failed to create headless template: %w", err)
		}
	}

	return nil, err
}
