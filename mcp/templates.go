package mcp

import (
	gocontext "context"
	"fmt"
	"strings"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/gomplate/v3"
	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/flanksource/incident-commander/auth"
)

const toolRunTemplate = "run_template"

type runTemplateArgs struct {
	Env           map[string]any `json:"env"`
	ChangeID      string         `json:"change_id"`
	ConfigID      string         `json:"config_id"`
	CheckID       string         `json:"check_id"`
	CELExpression string         `json:"cel_expression"`
	GoTemplate    string         `json:"gotemplate"`
}

func runTemplateHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	var args runTemplateArgs
	if err := req.BindArguments(&args); err != nil {
		return mcp.NewToolResultError("invalid arguments: " + err.Error()), nil
	}
	if args.Env == nil {
		args.Env = map[string]any{}
	}

	changeID := strings.TrimSpace(args.ChangeID)
	configID := strings.TrimSpace(args.ConfigID)
	checkID := strings.TrimSpace(args.CheckID)
	if changeID != "" || configID != "" || checkID != "" {
		err = auth.WithRLS(ctx, func(rlsCtx context.Context) error {
			if changeID != "" {
				if _, err := uuid.Parse(changeID); err != nil {
					return err
				}
				var change models.CatalogChange
				if err := rlsCtx.DB().Where("id = ?", changeID).Find(&change).Error; err != nil {
					return err
				}
				if change.ID == uuid.Nil {
					return fmt.Errorf("change[%s] not found", changeID)
				}
				args.Env["change"] = change.AsMap()
			}

			if configID != "" {
				if _, err := uuid.Parse(configID); err != nil {
					return err
				}
				config, err := query.GetCachedConfig(rlsCtx, configID)
				if err != nil {
					return err
				}
				if config == nil {
					return fmt.Errorf("config item[%s] not found", configID)
				}
				args.Env["config"] = config.AsMap()
			}

			if checkID != "" {
				if _, err := uuid.Parse(checkID); err != nil {
					return err
				}
				check, err := query.FindCachedCheck(rlsCtx, checkID)
				if err != nil {
					return err
				}
				if check == nil {
					return fmt.Errorf("check[%s] not found", checkID)
				}
				args.Env["check"] = check.AsMap()
			}

			return nil
		})
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
	}

	hasCEL := strings.TrimSpace(args.CELExpression) != ""
	hasGoTemplate := strings.TrimSpace(args.GoTemplate) != ""
	if hasCEL == hasGoTemplate {
		return mcp.NewToolResultError("provide exactly one of cel_expression or gotemplate"), nil
	}

	template := gomplate.Template{}
	if hasCEL {
		template.Expression = args.CELExpression
	} else {
		template.Template = args.GoTemplate
	}

	res, err := ctx.RunTemplate(template, args.Env)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	return structToMCPResponse(req, res), nil
}

func templateToolDescription() string {
	funcs := gomplate.ListAllFuncs()
	return "Evaluate a CEL expression or Go template against the provided env map and return the rendered string. " +
		"Provide exactly one of cel_expression or gotemplate.\n\n" +
		"Available functions (CEL + Go template):\n" + strings.Join(funcs, "\n")
}

func registerTemplates(s *server.MCPServer) {
	s.AddTool(mcp.NewTool(toolRunTemplate,
		mcp.WithDescription(templateToolDescription()),
		mcp.WithObject("env",
			mcp.Description("Environment map available to the expression/template."),
			mcp.AdditionalProperties(true),
		),
		mcp.WithString("change_id",
			mcp.Description("Optional catalog change UUID to fetch and inject into env as `change`."),
		),
		mcp.WithString("config_id",
			mcp.Description("Optional config item UUID to fetch and inject into env as `config`."),
		),
		mcp.WithString("check_id",
			mcp.Description("Optional check UUID to fetch and inject into env as `check`."),
		),
		mcp.WithString("cel_expression",
			mcp.Description("CEL expression to evaluate against env."),
		),
		mcp.WithString("gotemplate",
			mcp.Description("Go text/template to render using env."),
		),
	), runTemplateHandler)
}
