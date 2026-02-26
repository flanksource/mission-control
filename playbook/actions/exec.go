package actions

import (
	"github.com/flanksource/artifacts"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/shell"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/sandbox-runtime/sandbox"
)

type ExecAction struct {
}

type ExecDetails shell.ExecDetails

var defaultSandbox = sandbox.Config{
	Network: sandbox.NetworkConfig{
		AllowedDomains: []string{"flanksource.com", "*.flanksource.com"},
		DeniedDomains:  []string{},
	},
	Filesystem: sandbox.FilesystemConfig{
		DenyRead:   []string{},
		AllowWrite: []string{"/tmp"},
		DenyWrite:  []string{},
	},
}

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
	sb, err := sandbox.New(ctx, defaultSandbox)
	if err != nil {
		logger.Errorf("failed to create sandbox: %v", err)
	} else {
		defer sb.Close(ctx)
	}

	details, err := shell.Run(ctx, exec.ToShellExec(sb))
	return (*ExecDetails)(details), err
}
