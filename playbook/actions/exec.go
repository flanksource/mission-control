package actions

import (
	"github.com/flanksource/artifacts"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/shell"
	v1 "github.com/flanksource/incident-commander/api/v1"
)

type ExecAction struct {
}

type ExecDetails shell.ExecDetails

func (e *ExecDetails) GetArtifacts() []artifacts.Artifact {
	if e == nil {
		return nil
	}
	return e.Artifacts
}

func (e *ExecDetails) GetStatus() models.PlaybookActionStatus {
	if e.ExitCode != 0 {
		return models.PlaybookActionStatusFailed
	}

	return models.PlaybookActionStatusCompleted
}

func (c *ExecAction) Run(ctx context.Context, exec v1.ExecAction) (*ExecDetails, error) {
	details, err := shell.Run(ctx, exec.ToShellExec())
	return (*ExecDetails)(details), err
}
