package runner

import (
	gocontext "context"
	"fmt"

	"github.com/flanksource/artifacts"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/playbook/actions"
	"github.com/google/uuid"
	"github.com/samber/oops"
)

type ArtifactAccessor interface {
	GetArtifacts() []artifacts.Artifact
}

type StatusAccessor interface {
	GetStatus() models.PlaybookActionStatus
}

// executeActionResult is the result of executing an action
type executeActionResult struct {
	// result of the action as JSON
	data any

	// skipped is true if the action was skipped by the action filter
	skipped bool
}

// executeAction runs the executes the given palybook action.
// It should received an already templated action spec.
func executeAction(ctx context.Context, playbookID any, runID uuid.UUID, runAction models.PlaybookRunAction, actionSpec v1.PlaybookAction) (executeActionResult, error) {

	if timeout, _ := actionSpec.TimeoutDuration(); timeout > 0 {
		var cancel gocontext.CancelFunc
		ctx, cancel = ctx.WithTimeout(timeout)
		defer cancel()
	}

	if actionSpec.Filter != "" {
		if skipped, err := filterAction(ctx, runID, actionSpec.Filter); err != nil {
			return executeActionResult{}, err
		} else if skipped {
			ctx.Debugf("skipping %s", actionSpec.Name)
			return executeActionResult{skipped: true}, nil
		}
	}

	ctx.Debugf("executing %s", actionSpec.Name)

	var result any
	var err error

	if actionSpec.AzureDevopsPipeline != nil {
		var e actions.AzureDevopsPipeline
		result, err = e.Run(ctx, *actionSpec.AzureDevopsPipeline)
	} else if actionSpec.Github != nil {
		var e actions.Github
		result, err = e.Run(ctx, *actionSpec.Github)
	} else if actionSpec.Exec != nil {
		var e actions.ExecAction
		result, err = e.Run(ctx, *actionSpec.Exec)
	} else if actionSpec.HTTP != nil {
		var e actions.HTTP
		result, err = e.Run(ctx, *actionSpec.HTTP)
	} else if actionSpec.SQL != nil {
		var e actions.SQL
		result, err = e.Run(ctx, *actionSpec.SQL)
	} else if actionSpec.Pod != nil {
		e := actions.Pod{
			PlaybookRunID: runID,
			PlaybookID:    stringOrUuid(playbookID),
		}

		timeout, _ := actionSpec.TimeoutDuration()
		result, err = e.Run(ctx, *actionSpec.Pod, timeout)
	}

	switch v := result.(type) {
	case ArtifactAccessor:
		if err := saveArtifacts(ctx, runAction.ID, v.GetArtifacts()); err != nil {
			return executeActionResult{
				data: result,
			}, ctx.Oops().Wrapf(err, "error saving artifacts")
		}
	}

	if actionSpec.GitOps != nil {
		var e = actions.GitOps{Context: ctx}
		result1, err2 := e.Run(ctx, *actionSpec.GitOps)
		if result != nil {
			result = []any{result, result1}
		} else {
			result = result1
		}

		if err != nil && err2 != nil {
			err = oops.Join(err, err2)
		} else if err2 != nil {
			err = err2
		}
	}

	// notifications can run standalone or as part of another step
	if actionSpec.Notification != nil {
		var e actions.Notification
		err2 := e.Run(ctx, *actionSpec.Notification)
		if err != nil && err2 != nil {
			err = oops.Join(err, err2)
		} else if err2 != nil {
			err = err2
		}

	}

	results := executeActionResult{
		data: result,
	}

	return results, err
}

func stringOrUuid(id any) uuid.UUID {
	switch v := id.(type) {
	case uuid.UUID:
		return v
	}

	v, _ := uuid.Parse(fmt.Sprintf("%v", id))
	return v
}
