package playbook

import (
	"github.com/flanksource/commons/collections"
	"github.com/flanksource/duty/models"
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

	approval := models.PlaybookApproval{
		RunID: runID,
	}

	if collections.Contains(playbookV1.Spec.Approval.Approvers.People, approverID.String()) {
		approval.PersonID = &approverID
	} else {
		teamIDs, err := db.GetTeamIDsForUser(ctx, approverID.String())
		if err != nil {
			return api.Errorf(api.EINTERNAL, "something went wrong").WithDebugInfo("db.GetTeamIDsForUser(id=%s): %v", approverID, err)
		}

		for _, teamID := range teamIDs {
			if collections.Contains(playbookV1.Spec.Approval.Approvers.Teams, teamID.String()) {
				approval.TeamID = &teamID
				break
			}
		}

		if approval.TeamID == nil {
			return api.Errorf(api.EFORBIDDEN, "you are not allowed to approve this playbook run")
		}
	}

	if _, err := db.GetPlaybookRun(ctx, runID.String()); err != nil {
		return err
	}

	if err := db.SavePlaybookRunApproval(ctx, approval); err != nil {
		return api.Errorf(api.EINTERNAL, "something went wrong while approving").WithDebugInfo("db.ApprovePlaybookRun(runID=%s, approverID=%s): %v", runID, approverID, err)
	}

	return nil
}
