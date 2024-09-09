package runner

import (
	"encoding/json"

	"github.com/flanksource/commons/collections"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/gomplate/v3"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/playbook/actions"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/samber/oops"
)

func CreateTemplateEnv(ctx context.Context, playbook *models.Playbook, run *models.PlaybookRun) (actions.TemplateEnv, error) {
	templateEnv := actions.TemplateEnv{
		Params:   make(map[string]any, len(run.Parameters)),
		Run:      *run,
		Playbook: *playbook,
		Request:  run.Request,
		Env:      make(map[string]any),
	}

	oops := oops.With(models.ErrorContext(playbook, run)...)

	var spec v1.PlaybookSpec
	if err := json.Unmarshal(playbook.Spec, &spec); err != nil {
		return templateEnv, oops.Wrapf(err, "invalid playbook spec")
	}

	for _, p := range spec.Parameters {
		val, ok := run.Parameters[p.Name]
		if !ok || lo.IsEmpty(val) {
			// The parameter was not sent in the run.
			continue
		}

		switch p.Type {
		case v1.PlaybookParameterTypeCheck:
			var check models.Check
			if err := ctx.DB().Where("id = ?", val).First(&check).Error; err != nil {
				return templateEnv, oops.Tags("db").Wrap(err)
			} else if check.ID != uuid.Nil {
				templateEnv.Params[p.Name] = check.AsMap()
			}

		case v1.PlaybookParameterTypeConfig:
			var config models.ConfigItem
			if err := ctx.DB().Where("id = ?", val).First(&config).Error; err != nil {
				return templateEnv, oops.Tags("db").Wrap(err)
			} else if config.ID != uuid.Nil {
				templateEnv.Params[p.Name] = config.AsMap()
			}

		case v1.PlaybookParameterTypeComponent:
			var component models.Component
			if err := ctx.DB().Where("id = ?", val).First(&component).Error; err != nil {
				return templateEnv, oops.Tags("db").Wrap(err)
			} else if component.ID != uuid.Nil {
				templateEnv.Params[p.Name] = component.AsMap()
			}

		default:
			templateEnv.Params[p.Name] = val
		}
	}

	if run.CreatedBy != nil {
		if creator, err := query.FindPerson(ctx, run.CreatedBy.String()); err != nil {
			return templateEnv, oops.Tags("db").Wrap(err)
		} else if creator == nil {
			return templateEnv, oops.Errorf("playbook creator (id:%s)  not found", run.CreatedBy.String())
		} else {
			templateEnv.User = creator
		}
	}

	var resourceAgentID uuid.UUID
	if run.ComponentID != nil {
		if err := ctx.DB().Where("id = ?", run.ComponentID).First(&templateEnv.Component).Error; err != nil {
			return templateEnv, oops.Tags("db").Wrap(err)
		}
		resourceAgentID = templateEnv.Component.AgentID
	} else if run.ConfigID != nil {
		if err := ctx.DB().Where("id = ?", run.ConfigID).First(&templateEnv.Config).Error; err != nil {
			return templateEnv, oops.Tags("db").Wrap(err)
		}
		resourceAgentID = templateEnv.Config.AgentID
	} else if run.CheckID != nil {
		if err := ctx.DB().Where("id = ?", run.CheckID).First(&templateEnv.Check).Error; err != nil {
			return templateEnv, oops.Tags("db").Wrap(err)
		}
		resourceAgentID = templateEnv.Check.AgentID
	}

	if resourceAgentID != uuid.Nil {
		agent, err := query.FindCachedAgent(ctx, resourceAgentID.String())
		if err != nil {
			return templateEnv, oops.Tags("db").Wrap(err)
		} else if agent != nil {
			templateEnv.Agent = agent
		}
	}

	// isParamRenderCall is true when we are crafting the template env
	// just to render the parameters.
	// i.e. for a dummy run.
	isParamRenderCall := run.ID == uuid.Nil

	if !isParamRenderCall {
		if gitOpsEnvVar, err := getGitOpsTemplateVars(ctx, *run, spec.Actions); err != nil {
			return templateEnv, oops.Wrapf(err, "failed to get gitops vars")
		} else if gitOpsEnvVar != nil {
			templateEnv.Env = collections.MergeMap(templateEnv.Env, gitOpsEnvVar.AsMap())
		}
	}

	return templateEnv, nil
}

// templateAction templates all the cel-expressions in the action
func templateActionExpressions(ctx context.Context, run *models.PlaybookRun, runAction *models.PlaybookRunAction, actionSpec *v1.PlaybookAction, env actions.TemplateEnv) error {
	if actionSpec.Filter != "" {
		gomplateTemplate := gomplate.Template{
			Expression: actionSpec.Filter,
			CelEnvs:    getActionCelEnvs(ctx, run, runAction),
		}
		var err error
		if actionSpec.Filter, err = ctx.RunTemplate(gomplateTemplate, env.AsMap()); err != nil {
			return oops.With(models.ErrorContext(run, runAction)...).Wrapf(err, "failed to parse action filter (%s)", actionSpec.Filter)
		}
	}

	return nil
}

// TemplateAction all the go templates in the action
func TemplateAction(ctx context.Context, run *models.PlaybookRun, runAction *models.PlaybookRunAction, actionSpec *v1.PlaybookAction, env actions.TemplateEnv) error {
	templateFuncs := map[string]any{
		"getLastAction": func() any {
			r, err := GetLastAction(ctx, run.ID.String(), runAction.ID.String())
			if err != nil {
				ctx.Errorf("failed to get last action: %v", err)
				return ""
			}

			return r
		},
		"getAction": func(actionName string) any {
			r, err := GetActionByName(ctx, run.ID.String(), actionName)
			if err != nil {
				ctx.Errorf("failed to get action(%s) %v", actionName, err)
				return ""
			}

			return r
		},
	}
	templater := ctx.NewStructTemplater(env.AsMap(), "template", templateFuncs)
	return templater.Walk(&actionSpec)
}
