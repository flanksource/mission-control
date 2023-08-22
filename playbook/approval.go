package playbook

import (
	"github.com/flanksource/commons/collections"
	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/google/uuid"
)

func ApproveRun(ctx *api.Context, approverID, playbookID, runID uuid.UUID) error {
	playbook, err := db.FindPlaybook(ctx, playbookID)
	if err != nil {
		return api.Errorf(api.EINTERNAL, "something went wrong while finding playbook(id=%s)", playbookID).WithDebugInfo("db.FindPlaybook(id=%s): %v", playbookID, err)
	} else if playbook == nil {
		return api.Errorf(api.ENOTFOUND, "playbook(id=%s) not found", playbookID)
	}

	playbookV1, err := v1.PlaybookFromModel(*playbook)
	if err != nil {
		return api.Errorf(api.EINTERNAL, "something went wrong").WithDebugInfo("v1.PlaybookFromModel: %v", err)
	}

	if playbookV1.Spec.Approval == nil || playbookV1.Spec.Approval.Approvers.Empty() {
		return api.Errorf(api.EINVALID, "this playbook does not require approval")
	}

	if !collections.Contains(playbookV1.Spec.Approval.Approvers.IDs(), approverID.String()) {
		return api.Errorf(api.EFORBIDDEN, "you are not allowed to approve this playbook")
	}

	run, err := db.FindPlaybookRun(ctx, runID.String())
	if err != nil {
		return api.Errorf(api.EINTERNAL, "something went wrong while finding playbook run(id=%s)", runID).WithDebugInfo("db.FindPlaybookRun(id=%s): %v", runID, err)
	} else if run == nil {
		return api.Errorf(api.ENOTFOUND, "playbook run(id=%s) not found", runID)
	}

	if err := db.ApprovePlaybookRun(ctx, runID, &approverID, nil); err != nil {
		return api.Errorf(api.EINTERNAL, "something went wrong while approving").WithDebugInfo("db.ApprovePlaybookRun(runID=%s, approverID=%s): %v", runID, approverID, err)
	}

	return nil
}
