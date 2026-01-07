package mcp

import (
	gocontext "context"
	"strings"

	"github.com/flanksource/gomplate/v3"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const toolRunTemplate = "run_template"

type runTemplateArgs struct {
	Env           map[string]any `json:"env"`
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

const description = `Evaluate a CEL expression or Go template against the provided env map and return the rendered string. 
Provide exactly one of cel_expression or gotemplate.`

func registerTemplates(s *server.MCPServer) {
	s.AddTool(mcp.NewTool(toolRunTemplate,
		mcp.WithDescription(description),
		mcp.WithObject("env",
			mcp.Description("Environment map available to the expression/template."),
			mcp.AdditionalProperties(true),
		),
		mcp.WithString("cel_expression",
			mcp.Description("CEL expression to evaluate against env."),
		),
		mcp.WithString("gotemplate",
			mcp.Description("Go text/template to render using env."),
		),
	), runTemplateHandler)
}
