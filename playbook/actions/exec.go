package actions

import (
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/shell"
	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
)

type ExecAction struct {
}

type ExecDetails = api.PlaybookExecResult

func (c *ExecAction) Run(ctx context.Context, exec v1.ExecAction) (*ExecDetails, error) {
	details, err := shell.Run(ctx, exec.ToShellExec())
	return (*ExecDetails)(details), err
}
