package playbook

import (
	"github.com/flanksource/commons/collections"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/google/uuid"
)

func ApproveRun(ctx *api.Context, playbookID, runID uuid.UUID) error {
	approver := ctx.User()
	if approver == nil {
		return api.Errorf(api.EUNAUTHORIZED, "user not found.")
	}

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

	if collections.Contains(playbookV1.Spec.Approval.Approvers.People, approver.Email) {
		approval.PersonID = &approver.ID
	} else {
		teams, err := db.GetTeamsForUser(ctx, approver.ID.String())
		if err != nil {
			return api.Errorf(api.EINTERNAL, "something went wrong").WithDebugInfo("db.GetTeamIDsForUser(id=%s): %v", approver.ID, err)
		}

		for _, team := range teams {
			if collections.Contains(playbookV1.Spec.Approval.Approvers.Teams, team.Name) {
				approval.TeamID = &team.ID
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
		return api.Errorf(api.EINTERNAL, "something went wrong while approving").WithDebugInfo("db.ApprovePlaybookRun(runID=%s, approverID=%s): %v", runID, approver.ID, err)
	}

	return nil
}
