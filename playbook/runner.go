package playbook

import (
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
)

// Run runs the requested playbook with the provided parameters.
func Run(ctx *api.Context, playbook models.Playbook, req RunParams) (*models.PlaybookRun, error) {
	run := models.PlaybookRun{
		PlaybookID: playbook.ID,
		// CreatedBy:  ctx.User().ID, // TODO: Add user id to the context
	}
	if err := ctx.DB().Create(&run).Error; err != nil {
		return nil, err
	}

	// For now run in go routine.
	// Might need to implement a runner.

	return &run, nil
}
