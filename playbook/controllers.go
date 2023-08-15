package playbook

import (
	"fmt"
	"net/http"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

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

// Run runs the requested playbook with the provided parameters.
func Run(c echo.Context) error {
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

	// TODO: Run the playbook
	// Return the playbook run ID and exit without waiting for the run to finish.
	// The user will query the status of the playbook run via the another endpoint using
	// the playbook run ID.

	return nil
}
