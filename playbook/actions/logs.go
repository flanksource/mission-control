package actions

import (
	"io"
	"strings"

	"github.com/flanksource/artifacts"
	"github.com/flanksource/duty/context"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/logs/loki"
)

type logsAction struct {
}

type logsResult struct {
	Metadata any `json:"metadata,omitempty"`

	// Saved to artifacts
	logs string
}

func (t *logsResult) GetArtifacts() []artifacts.Artifact {
	return []artifacts.Artifact{
		{
			ContentType: "markdown",
			Content:     io.NopCloser(strings.NewReader(string(t.logs))),
			Path:        "logs.json",
		},
	}
}

func NewLogsAction() *logsAction {
	return &logsAction{}
}

func (l *logsAction) Run(ctx context.Context, action *v1.LogsAction) (*logsResult, error) {
	if action.Loki != nil {
		response, err := loki.Fetch(ctx, action.Loki.BaseURL, action.Loki.Authentication, action.Loki.Request)
		if err != nil {
			return nil, ctx.Oops().Wrapf(err, "failed to fetch logs")
		}

		return &logsResult{
			Metadata: response.Data.Stats,
			logs:     string(response.Data.Result),
		}, nil
	}

	return nil, nil
}
