package events

import (
	"encoding/json"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
)

func NewPlaybookApprovalSpecUpdatedConsumerSync() SyncEventConsumer {
	return SyncEventConsumer{
		watchEvents: []string{EventPlaybookSpecApprovalUpdated},
		consumers:   []SyncEventHandlerFunc{onApprovalUpdated},
	}
}

func NewPlaybookApprovalConsumerSync() SyncEventConsumer {
	return SyncEventConsumer{
		watchEvents: []string{EventPlaybookApprovalInserted},
		consumers:   []SyncEventHandlerFunc{onPlaybookRunNewApproval},
	}
}

func schedulePlaybookRun(ctx *api.Context, event api.Event) error {
	// TODO:
	// See if any playbook is listening on this event.
	// Match the filters
	// If everything goes ok, save the playbook run.
	return nil
}

func onApprovalUpdated(ctx *api.Context, event api.Event) error {
	playbookID := event.Properties["id"]

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

func onPlaybookRunNewApproval(ctx *api.Context, event api.Event) error {
	runID := event.Properties["run_id"]

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
