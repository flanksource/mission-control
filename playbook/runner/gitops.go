package runner

import (
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	v1 "github.com/flanksource/incident-commander/api/v1"
)

func getGitOpsTemplateVars(ctx context.Context, run models.PlaybookRun, actions []v1.PlaybookAction) (*query.GitOpsSource, error) {
	if run.ConfigID == nil {
		return nil, nil
	}

	var hasGitOpsAction bool
	for _, action := range actions {
		if action.GitOps != nil {
			hasGitOpsAction = true
			break
		}
	}

	if !hasGitOpsAction {
		return nil, nil
	}

	source, err := query.GetGitOpsSource(ctx, *run.ConfigID)
	return &source, err
}
