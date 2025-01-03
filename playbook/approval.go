package playbook

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/flanksource/commons/collections"
	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/rbac"
)

func HandlePlaybookRunApproval(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	var (
		runID = c.Param("run_id")
	)

	runUUID, err := uuid.Parse(runID)
	if err != nil {
		return c.JSON(http.StatusBadRequest, api.HTTPError{Err: err.Error(), Message: "invalid run id"})
	}

	if err := ApproveRun(ctx, runUUID); err != nil {
		return api.WriteError(c, err)
	}

	return c.JSON(http.StatusOK, api.HTTPSuccess{Message: "playbook run approved"})
}

func ApproveRun(ctx context.Context, runID uuid.UUID) error {
	run, err := db.FindPlaybookRun(ctx, runID)
	if err != nil {
		return api.Errorf(api.EINTERNAL, "something went wrong while finding run (id=%s)", runID).WithDebugInfo("db.FindPlaybookRun(id=%s): %v", runID, err)
	} else if run == nil {
		return api.Errorf(api.ENOTFOUND, "playbook run (id=%s) not found", runID)
	}

	return approveRun(ctx, run)
}

func requiresApproval(spec v1.PlaybookSpec) bool {
	return spec.Approval != nil && !spec.Approval.Approvers.Empty()
}

func approveRun(ctx context.Context, run *models.PlaybookRun) error {
	approver := ctx.User()
	if objects, err := run.GetRBACAttributes(ctx.DB()); err != nil {
		return ctx.Oops().Wrap(err)
	} else if !rbac.HasPermission(ctx, approver.ID.String(), objects, rbac.ActionPlaybookApprove) {
		return ctx.Oops().With("permission", rbac.ActionPlaybookApprove, "objects", objects).Code(api.EFORBIDDEN).Wrap(errors.New("access denied: approval permission required"))
	}

	var spec v1.PlaybookSpec
	if err := json.Unmarshal(run.Spec, &spec); err != nil {
		return err
	}

	if spec.Approval == nil || spec.Approval.Approvers.Empty() {
		return api.Errorf(api.EINVALID, "this playbook does not require approval")
	}

	if approver == nil {
		return api.Errorf(api.EUNAUTHORIZED, "Not logged in")
	}

	approval := models.PlaybookApproval{
		RunID: run.ID,
	}

	if collections.Contains(spec.Approval.Approvers.People, approver.Email) {
		approval.PersonID = &approver.ID
	} else {
		teams, err := db.GetTeamsForUser(ctx, approver.ID.String())
		if err != nil {
			return api.Errorf(api.EINTERNAL, "something went wrong").WithDebugInfo("db.GetTeamsForUser(id=%s): %v", approver.ID, err)
		}

		for _, team := range teams {
			if collections.Contains(spec.Approval.Approvers.Teams, team.Name) {
				approval.TeamID = &team.ID
				break
			}
		}

		if approval.TeamID == nil {
			return api.Errorf(api.EFORBIDDEN, "you are not allowed to approve this playbook run")
		}
	}

	if err := db.SavePlaybookRunApproval(ctx, approval); err != nil {
		return api.Errorf(api.EINTERNAL, "something went wrong while approving").WithDebugInfo("db.SavePlaybookRunApproval(runID=%s, approverID=%s): %v", run.ID, approver.ID, err)
	}

	return nil
}
