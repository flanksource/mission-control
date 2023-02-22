package db

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

const (
	dummyTemplateName      = "dummy"
	dummyTemplateNamespace = "dummy"
)

func CreateDummyTemplate(ctx context.Context) error {
	tx := Gorm.WithContext(ctx).Exec(`INSERT INTO templates (name, namespace) VALUES(?, ?) ON CONFLICT DO NOTHING`, dummyTemplateName, dummyTemplateNamespace)
	if tx.Error != nil {
		return fmt.Errorf("failed to create dummy template: %w", tx.Error)
	}

	return nil
}

func GetDummyTemplateID(ctx context.Context) (*uuid.UUID, error) {
	var id string
	tx := Gorm.WithContext(ctx).Raw(`SELECT id FROM templates WHERE name = ? AND namespace = ?`, dummyTemplateName, dummyTemplateNamespace).Scan(&id)
	if tx.Error != nil {
		return nil, fmt.Errorf("failed to select dummy template: %w", tx.Error)
	}

	x := uuid.MustParse(id)
	return &x, nil
}
