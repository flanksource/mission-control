package playbook

import (
	"net/http"

	"github.com/flanksource/commons/collections"
	"github.com/flanksource/duty/api"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
)

func HandlePlaybookRunApproval(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	var (
		playbookID = c.Param("playbook_id")
		runID      = c.Param("run_id")
	)

	playbookUUID, err := uuid.Parse(playbookID)
	if err != nil {
		return c.JSON(http.StatusBadRequest, dutyAPI.HTTPError{Err: err.Error(), Message: "invalid playbook id"})
	}

	runUUID, err := uuid.Parse(runID)
	if err != nil {
		return c.JSON(http.StatusBadRequest, dutyAPI.HTTPError{Err: err.Error(), Message: "invalid run id"})
	}

	if err := ApproveRun(ctx, playbookUUID, runUUID); err != nil {
		return dutyAPI.WriteError(c, err)
	}

	return c.JSON(http.StatusOK, dutyAPI.HTTPSuccess{Message: "playbook run approved"})
}

func ApproveRun(ctx context.Context, playbookID, runID uuid.UUID) error {
	playbook, err := db.FindPlaybook(ctx, playbookID)
	if err != nil {
		return api.Errorf(api.EINTERNAL, "something went wrong while finding playbook(id=%s)", playbookID).WithDebugInfo("db.FindPlaybook(id=%s): %v", playbookID, err)
	} else if playbook == nil {
		return api.Errorf(api.ENOTFOUND, "playbook(id=%s) not found", playbookID)
	}

	return approveRun(ctx, playbook, runID)
}

func requiresApproval(playbook *models.Playbook) bool {
	playbookV1, _ := v1.PlaybookFromModel(*playbook)
	return playbookV1.Spec.Approval != nil && !playbookV1.Spec.Approval.Approvers.Empty()
}

func approveRun(ctx context.Context, playbook *models.Playbook, runID uuid.UUID) error {
	approver := ctx.User()

	playbookV1, err := v1.PlaybookFromModel(*playbook)
	if err != nil {
		return api.Errorf(api.EINTERNAL, "something went wrong").WithDebugInfo("v1.PlaybookFromModel: %v", err)
	}

	if playbookV1.Spec.Approval == nil || playbookV1.Spec.Approval.Approvers.Empty() {
		return api.Errorf(api.EINVALID, "this playbook does not require approval")
	}

	if approver == nil {
		return api.Errorf(api.EUNAUTHORIZED, "Not logged in")
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

	if err := db.SavePlaybookRunApproval(ctx, approval); err != nil {
		return api.Errorf(api.EINTERNAL, "something went wrong while approving").WithDebugInfo("db.ApprovePlaybookRun(runID=%s, approverID=%s): %v", runID, approver.ID, err)
	}

	return nil
}
