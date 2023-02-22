package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// dummyTemplateNamespace is a shared template namespace for
// downstream instances.
const dummyTemplateNamespace = "push"

func getDummyTemplate(ctx context.Context, name string) (*models.SystemTemplate, error) {
	template := models.SystemTemplate{Name: name, Namespace: dummyTemplateNamespace}
	tx := Gorm.WithContext(ctx).Where(template).First(&template)
	return &template, tx.Error
}

func createDummyTemplate(ctx context.Context, name string) (*models.SystemTemplate, error) {
	template := models.SystemTemplate{ID: uuid.New(), Name: name, Namespace: dummyTemplateNamespace}
	tx := Gorm.WithContext(ctx).Create(&template)
	return &template, tx.Error
}

func GetOrCreateDummyTemplateID(ctx context.Context, name string) (*models.SystemTemplate, error) {
	id, err := getDummyTemplate(ctx, name)
	if nil == err {
		return id, nil
	}

	if errors.Is(err, gorm.ErrRecordNotFound) {
		newDummyTpl, err := createDummyTemplate(ctx, name)
		if nil == err {
			return newDummyTpl, nil
		}

		if err != nil {
			return nil, fmt.Errorf("failed to create dummy template: %w", err)
		}
	}

	return nil, err
}
