package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/flanksource/duty/models"
	"gorm.io/gorm"
)

// headlessTopologyNamespace is a shared topology namespace for
// downstream instances.
const headlessTopologyNamespace = "push"

func getHeadlessTopology(ctx context.Context, name string) (*models.Topology, error) {
	t := models.Topology{Name: name, Namespace: headlessTopologyNamespace}
	tx := Gorm.WithContext(ctx).Where(t).First(&t)
	return &t, tx.Error
}

func createHeadlessTopology(ctx context.Context, name string) (*models.Topology, error) {
	t := models.Topology{Name: name, Namespace: headlessTopologyNamespace}
	tx := Gorm.WithContext(ctx).Create(&t)
	return &t, tx.Error
}

func GetOrCreateHeadlessTopology(ctx context.Context, name string) (*models.Topology, error) {
	t, err := getHeadlessTopology(ctx, name)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			newHeadlessTopology, err := createHeadlessTopology(ctx, name)
			if err != nil {
				return nil, fmt.Errorf("failed to create headless topology: %w", err)
			}
			return newHeadlessTopology, nil
		}
		return nil, err
	}

	return t, nil
}
