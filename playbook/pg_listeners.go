package playbook

import (
	"encoding/json"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/utils"
	"github.com/jackc/pgx/v5/pgxpool"
	"gorm.io/gorm"
)

func ListenPlaybookPGNotify(db *gorm.DB, pool *pgxpool.Pool) {
	var (
		dbReconnectMaxDuration         = time.Minute
		dbReconnectBackoffBaseDuration = time.Second
	)

	pgNotifyPlaybookSpecApprovalUpdated := make(chan string)
	go utils.ListenToPostgresNotify(pool, "playbook_spec_approval_updated", dbReconnectMaxDuration, dbReconnectBackoffBaseDuration, pgNotifyPlaybookSpecApprovalUpdated)

	pgNotifyPlaybookApprovalsInserted := make(chan string)
	go utils.ListenToPostgresNotify(pool, "playbook_approval_inserted", dbReconnectMaxDuration, dbReconnectBackoffBaseDuration, pgNotifyPlaybookApprovalsInserted)

	ctx := api.NewContext(db, nil)
	for {
		select {
		case id := <-pgNotifyPlaybookSpecApprovalUpdated:
			if err := onApprovalUpdated(ctx, id); err != nil {
				logger.Errorf("%v", err)
			}

		case id := <-pgNotifyPlaybookApprovalsInserted:
			if err := onPlaybookRunNewApproval(ctx, id); err != nil {
				logger.Errorf("%v", err)
			}
		}
	}
}

// onApprovalUpdated is called when the playbook spec approval is updated
func onApprovalUpdated(ctx *api.Context, playbookID string) error {
	var playbook models.Playbook
	if err := ctx.DB().Where("id = ?", playbookID).First(&playbook).Error; err != nil {
		return err
	}

	var spec v1.PlaybookSpec
	if err := json.Unmarshal(playbook.Spec, &spec); err != nil {
		return err
	}

	if spec.Approval == nil {
		return nil
	}

	return db.UpdatePlaybookRunStatusIfApproved(ctx, playbookID, *spec.Approval)
}

func onPlaybookRunNewApproval(ctx *api.Context, runID string) error {
	var run models.PlaybookRun
	if err := ctx.DB().Where("id = ?", runID).First(&run).Error; err != nil {
		return err
	}

	if run.Status != models.PlaybookRunStatusPending {
		return nil
	}

	var playbook models.Playbook
	if err := ctx.DB().Where("id = ?", run.PlaybookID).First(&playbook).Error; err != nil {
		return err
	}

	var spec v1.PlaybookSpec
	if err := json.Unmarshal(playbook.Spec, &spec); err != nil {
		return err
	}

	if spec.Approval == nil {
		return nil
	}

	return db.UpdatePlaybookRunStatusIfApproved(ctx, playbook.ID.String(), *spec.Approval)
}
