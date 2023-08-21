package playbook

import (
	"fmt"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/google/uuid"
)

func ListPlaybooksOfConfig(ctx *api.Context, id string) ([]models.Playbook, error) {
	var config models.ConfigItem
	if err := ctx.DB().Where("id = ?", id).Find(&config).Error; err != nil {
		return nil, err
	} else if config.ID == uuid.Nil {
		return nil, fmt.Errorf("config(id=%s) not found", id)
	}

	return db.FindPlaybooksByTypeAndTags(ctx, *config.Type, *config.Tags)
}

func ListPlaybooksOfComponent(ctx *api.Context, id string) ([]models.Playbook, error) {
	var component models.Component
	if err := ctx.DB().Where("id = ?", id).Find(&component).Error; err != nil {
		return nil, err
	} else if component.ID == uuid.Nil {
		return nil, fmt.Errorf("component(id=%s) not found", id)
	}

	return db.FindPlaybooksByTypeAndTags(ctx, component.Type, component.Labels)
}
