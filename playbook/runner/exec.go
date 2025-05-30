package runner

import (
	gocontext "context"
	"fmt"
	"reflect"

	"github.com/flanksource/artifacts"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"github.com/google/uuid"
	"github.com/samber/oops"

	v1 "github.com/flanksource/incident-commander/api/v1"
	pkgArtifacts "github.com/flanksource/incident-commander/artifacts"
	"github.com/flanksource/incident-commander/playbook/actions"
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
func executeAction(ctx context.Context, playbookID any, runID uuid.UUID, runAction models.PlaybookRunAction, actionSpec v1.PlaybookAction, templateEnv actions.TemplateEnv) (executeActionResult, error) {
	if timeout, _ := actionSpec.TimeoutDuration(); timeout > 0 {
		var cancel gocontext.CancelFunc
		ctx, cancel = ctx.WithTimeout(timeout)
		defer cancel()
	}

	if actionSpec.Filter != "" {
		if skipped, err := filterAction(ctx, actionSpec.Filter); err != nil {
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
	} else if actionSpec.AI != nil {
		e := actions.NewAIAction(stringOrUuid(playbookID), runID, templateEnv)
		result, err = e.Run(ctx, *actionSpec.AI)
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
	} else if actionSpec.Logs != nil {
		if api.DefaultArtifactConnection == "" {
			return executeActionResult{}, ctx.Oops().
				Hint("https://flanksource.com/docs/installation/artifacts/#setting-up-artifact-store").
				Errorf("logs action requires an artifact connection to be configured.")
		}

		e := actions.NewLogsAction()
		result, err = e.Run(ctx, actionSpec.Logs)
	}

	// NOTE: v is never nil, it holds in nil values.
	// So we need to check if the value is nil using reflect.ValueOf.
	if v, ok := result.(ArtifactAccessor); ok && !reflect.ValueOf(v).IsNil() {
		if err := pkgArtifacts.SaveArtifacts(ctx, runAction.ID, v.GetArtifacts()); err != nil {
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
		var err2 error
		if result, err2 = e.Run(ctx, *actionSpec.Notification); err != nil && err2 != nil {
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
