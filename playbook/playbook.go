package playbook

import (
	"fmt"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/google/uuid"
)

// savePlaybookRun saves the run and attempts register an approval from the caller.
func savePlaybookRun(ctx context.Context, playbook *models.Playbook, run *models.PlaybookRun) error {
	tx := ctx.DB().Begin()
	if tx.Error != nil {
		return tx.Error
	}
	defer tx.Rollback()

	ctx = ctx.WithDB(tx, ctx.Pool())

	if err := ctx.DB().Create(run).Error; err != nil {
		return err
	}

	// Attempt to auto approve run
	if err := approveRun(ctx, playbook, run.ID); err != nil {
		switch api.ErrorCode(err) {
		case api.EFORBIDDEN, api.EINVALID:
			// ignore these errors
		default:
			return fmt.Errorf("error while attempting to auto approve run: %v", err)
		}
	}

	return tx.Commit().Error
}

func ListPlaybooksForConfig(ctx context.Context, id string) ([]api.PlaybookListItem, error) {
	var config models.ConfigItem
	if err := ctx.DB().Where("id = ?", id).Find(&config).Error; err != nil {
		return nil, err
	} else if config.ID == uuid.Nil {
		return nil, api.Errorf(api.ENOTFOUND, "config(id=%s) not found", id)
	}

	return db.FindPlaybooksForConfig(ctx, *config.Type, *config.Tags)
}

func ListPlaybooksForComponent(ctx context.Context, id string) ([]api.PlaybookListItem, error) {
	var component models.Component
	if err := ctx.DB().Where("id = ?", id).Find(&component).Error; err != nil {
		return nil, err
	} else if component.ID == uuid.Nil {
		return nil, api.Errorf(api.ENOTFOUND, "component(id=%s) not found", id)
	}

	return db.FindPlaybooksForComponent(ctx, component.Type, component.Labels)
}

func ListPlaybooksForCheck(ctx context.Context, id string) ([]api.PlaybookListItem, error) {
	var check models.Check
	if err := ctx.DB().Where("id = ?", id).Find(&check).Error; err != nil {
		return nil, err
	} else if check.ID == uuid.Nil {
		return nil, api.Errorf(api.ENOTFOUND, "check(id=%s) not found", id)
	}

	return db.FindPlaybooksForCheck(ctx, check.Type, check.Labels)
}
