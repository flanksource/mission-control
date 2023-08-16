package playbook

import (
	"fmt"
	"net/http"
	"time"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

type RunResponse struct {
	RunID    string `json:"run_id"`
	StartsAt string `json:"starts_at"`
}

type RunParams struct {
	ID          uuid.UUID         `json:"id"`
	ConfigID    uuid.UUID         `json:"config_id"`
	ComponentID uuid.UUID         `json:"component_id"`
	Params      map[string]string `json:"params"`
}

func (r *RunParams) Valid() error {
	if r.ID == uuid.Nil {
		return fmt.Errorf("playbook id is required")
	}

	if r.ConfigID == uuid.Nil && r.ComponentID == uuid.Nil {
		return fmt.Errorf("either config_id or component_id is required")
	}

	if r.ConfigID != uuid.Nil && r.ComponentID != uuid.Nil {
		return fmt.Errorf("either config_id or component_id is required")
	}

	return nil
}

// HandlePlaybookRun handles playbook run requests.
func HandlePlaybookRun(c echo.Context) error {
	ctx := c.(*api.Context)

	var req RunParams
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, api.HTTPError{Error: err.Error(), Message: "invalid request"})
	}

	if err := req.Valid(); err != nil {
		return c.JSON(http.StatusBadRequest, api.HTTPError{Error: err.Error(), Message: "invalid request"})
	}

	playbook, err := db.FindPlaybook(ctx, req.ID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{Error: err.Error(), Message: "failed to get playbook"})
	} else if playbook == nil {
		return c.JSON(http.StatusNotFound, api.HTTPError{Error: "not found", Message: fmt.Sprintf("playbook(id=%s) not found", req.ID)})
	}

	run := models.PlaybookRun{
		PlaybookID: playbook.ID,
		Status:     models.PlaybookRunStatusScheduled,
		// CreatedBy:  ctx.User().ID, // TODO: Add user id to the context
	}

	if req.ComponentID != uuid.Nil {
		run.ComponentID = &req.ComponentID
	}

	if req.ConfigID != uuid.Nil {
		run.ConfigID = &req.ConfigID
	}

	if err := ctx.DB().Create(&run).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{Error: err.Error(), Message: "failed to create playbook run"})
	}

	return c.JSON(http.StatusCreated, RunResponse{
		RunID:    run.ID.String(),
		StartsAt: run.StartDate.Format(time.RFC3339),
	})
}

func HandlePlaybookRunStatus(c echo.Context) error {
	ctx := c.(*api.Context)
	id := c.Param("id")

	run, err := db.FindPlaybookRun(ctx, id)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{Error: err.Error(), Message: "failed to get playbook run"})
	} else if run == nil {
		return c.JSON(http.StatusNotFound, api.HTTPError{Error: "not found", Message: fmt.Sprintf("playbook run(id=%s) not found", id)})
	}

	return c.JSON(http.StatusOK, run)
}
