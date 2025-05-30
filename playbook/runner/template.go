package runner

import (
	"encoding/json"

	"github.com/flanksource/commons/collections"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/secret"
	"github.com/flanksource/gomplate/v3"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/samber/oops"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/notification"
	"github.com/flanksource/incident-commander/playbook/actions"
)

// CreateTemplateEnv creates a template environment for the playbook run.
//
// NOTE: It shouldn't mutate the input arguments
// (example: setting the parameter value to playbook.parameters after templating)
func CreateTemplateEnv(ctx context.Context, playbook *models.Playbook, run models.PlaybookRun, action *models.PlaybookRunAction) (actions.TemplateEnv, error) {
	templateEnv := actions.TemplateEnv{
		Params:   make(map[string]any, len(run.Parameters)),
		Run:      run,
		Action:   action,
		Playbook: *playbook,
		Request:  run.Request,
		Env:      make(map[string]any),
	}

	oops := oops.With(models.ErrorContext(playbook, run)...)

	var spec v1.PlaybookSpec
	if err := json.Unmarshal(run.Spec, &spec); err != nil {
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

		case v1.PlaybookParameterTypeSecret:
			ciphertext, err := secret.ParseCiphertext(val)
			if err != nil {
				return templateEnv, oops.Wrapf(err, "failed to parse secret parameter (%s). not a valid cipher text", p.Name)
			}

			sensitive, err := secret.Decrypt(ctx, ciphertext)
			if err != nil {
				return templateEnv, oops.Wrapf(err, "failed to decrypt secret parameter (%s)", p.Name)
			}

			templateEnv.Params[p.Name] = sensitive

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

	if gitOpsEnvVar, err := getGitOpsTemplateVars(ctx, run, spec.Actions); err != nil {
		return templateEnv, oops.Wrapf(err, "failed to get gitops vars")
	} else if gitOpsEnvVar != nil {
		templateEnv.GitOps = *gitOpsEnvVar
	}

	env := make(map[string]any)
	for _, e := range spec.Env {
		val, err := ctx.GetEnvValueFromCache(e, ctx.GetNamespace())
		if err != nil {
			return templateEnv, ctx.Oops("env").Wrapf(err, "failed to get env[%s]", e.Name)
		} else {
			env[e.Name] = val

			val, err := ctx.RunTemplate(gomplate.Template{
				Template:  val,
				Functions: getGomplateFuncs(ctx, templateEnv),
			}, templateEnv.AsMap(ctx))
			if err != nil {
				return templateEnv, ctx.Oops().Wrap(err)
			}
			env[e.Name] = val
		}
	}

	templateEnv.Env = collections.MergeMap(templateEnv.Env, env)
	return templateEnv, nil
}

// templateAction templates all the cel-expressions in the action
func templateActionExpressions(ctx context.Context, actionSpec *v1.PlaybookAction, env actions.TemplateEnv) error {
	if actionSpec.Filter != "" {
		gomplateTemplate := gomplate.Template{
			Expression: actionSpec.Filter,
			CelEnvs:    getActionCelEnvs(ctx, env),
		}
		var err error
		if actionSpec.Filter, err = ctx.RunTemplate(gomplateTemplate, env.AsMap(ctx)); err != nil {
			return err
		}
	}

	return nil
}

func getGomplateFuncs(ctx context.Context, env actions.TemplateEnv) map[string]any {
	return map[string]any{
		"getLastAction": func() any {
			if env.Action == nil {
				return make(map[string]any)
			}
			r, err := GetLastAction(ctx, env.Run.ID.String(), env.Action.ID.String())
			if err != nil {
				ctx.Errorf("failed to get last action: %v", err)
				return make(map[string]any)
			}

			return r
		},
		"getAction": func(actionName string) any {
			r, err := GetActionByName(ctx, env.Run.ID.String(), actionName)
			if err != nil {
				ctx.Errorf("failed to get action(%s) %v", actionName, err)
				return make(map[string]any)
			}

			return r
		},
	}
}

// TemplateAction all the go templates in the action
func TemplateEnv(ctx context.Context, env actions.TemplateEnv, template string) (string, error) {
	return ctx.RunTemplate(gomplate.Template{Template: template, Functions: getGomplateFuncs(ctx, env)}, env.AsMap(ctx))
}

// TemplateAction all the go templates in the action
func TemplateAction(ctx context.Context, actionSpec *v1.PlaybookAction, env actions.TemplateEnv) error {
	templateFuncs := collections.MergeMap(getGomplateFuncs(ctx, env), notification.TemplateFuncs)
	templater := ctx.NewStructTemplater(env.AsMap(ctx), "template", templateFuncs)
	if err := templater.Walk(&actionSpec); err != nil {
		return ctx.Oops().Wrapf(err, "failed to template action")
	}

	// TODO: make this work with template.Walk()
	if actionSpec.Exec != nil && actionSpec.Exec.Connections.FromConfigItem != nil {
		if v, err := ctx.NewStructTemplater(env.AsMap(ctx), "", templateFuncs).
			Template(*actionSpec.Exec.Connections.FromConfigItem); err != nil {
			return ctx.Oops().Wrapf(err, "failed to template exec action connection from config item")
		} else {
			actionSpec.Exec.Connections.FromConfigItem = &v
		}
	}

	return nil
}
