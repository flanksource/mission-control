package actions

import (
	"fmt"
	"strings"

	"github.com/flanksource/artifacts"
	"github.com/flanksource/clicky"
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

func (e ExecDetails) String() string  { return e.plain(false) }
func (e ExecDetails) ANSI() string    { return e.plain(true) }
func (e ExecDetails) HTML() string    { return "<pre>" + e.plain(false) + "</pre>" }
func (e ExecDetails) Markdown() string { return "```\n" + e.plain(false) + "\n```" }

func (e ExecDetails) plain(colors bool) string {
	var b strings.Builder

	if e.Stdout != "" {
		if colors {
			b.WriteString(clicky.Text("Stdout:", "font-bold text-green-600").ANSI())
		} else {
			b.WriteString("Stdout:")
		}
		b.WriteString("\n")
		b.WriteString(strings.TrimSpace(e.Stdout))
		b.WriteString("\n")
	}

	if e.Stderr != "" {
		if colors {
			b.WriteString(clicky.Text("Stderr:", "font-bold text-red-600").ANSI())
		} else {
			b.WriteString("Stderr:")
		}
		b.WriteString("\n")
		b.WriteString(strings.TrimSpace(e.Stderr))
		b.WriteString("\n")
	}

	if e.ExitCode != 0 {
		if colors {
			b.WriteString(clicky.Text(fmt.Sprintf("Exit Code: %d", e.ExitCode), "text-red-600").ANSI())
		} else {
			b.WriteString(fmt.Sprintf("Exit Code: %d", e.ExitCode))
		}
		b.WriteString("\n")
	}

	return strings.TrimRight(b.String(), "\n")
}
