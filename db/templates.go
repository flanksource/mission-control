package db

import (
	"errors"
	"fmt"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"gorm.io/gorm"
)

// headlessTopologyNamespace is a shared topology namespace for
// downstream instances.
const headlessTopologyNamespace = "push"

func getHeadlessTopology(ctx *api.Context, name string) (*models.Topology, error) {
	t := models.Topology{Name: name, Namespace: headlessTopologyNamespace}
	tx := ctx.DB().Where(t).First(&t)
	return &t, tx.Error
}

func createHeadlessTopology(ctx *api.Context, name string) (*models.Topology, error) {
	t := models.Topology{Name: name, Namespace: headlessTopologyNamespace}
	tx := ctx.DB().Create(&t)
	return &t, tx.Error
}

func GetOrCreateHeadlessTopology(ctx *api.Context, name string) (*models.Topology, error) {
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
