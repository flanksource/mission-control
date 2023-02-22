package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// dummyTemplateNamespace is a shared template namespace for
// downstream instances.
const dummyTemplateNamespace = "push"

// TODO: This should be in duty
type Template struct {
	ID        uuid.UUID `gorm:"column:id"`
	Name      string    `gorm:"column:name"`
	Namespace string    `gorm:"column:namespace"`
}

func getDummyTemplate(ctx context.Context, name string) (*Template, error) {
	template := Template{Name: name, Namespace: dummyTemplateNamespace}
	tx := Gorm.WithContext(ctx).Where(template).First(&template)
	return &template, tx.Error
}

func createDummyTemplate(ctx context.Context, name string) (*Template, error) {
	template := Template{ID: uuid.New(), Name: name, Namespace: dummyTemplateNamespace}
	tx := Gorm.WithContext(ctx).Create(&template)
	return &template, tx.Error
}

func GetOrCreateDummyTemplateID(ctx context.Context, name string) (*Template, error) {
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
