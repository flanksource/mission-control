package mcp

import (
	gocontext "context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/playbook"
	"github.com/invopop/jsonschema"
	"github.com/labstack/echo/v4"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/samber/lo"
	orderedmap "github.com/wk8/go-ordered-map/v2"
	"gorm.io/gorm"
)

func playbookRecentRunHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	limit := req.GetInt("limit", 20)

	pbrs, err := db.GetRecentPlaybookRuns(ctx, limit)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	jsonData, err := json.Marshal(pbrs)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	return mcp.NewToolResultText(string(jsonData)), nil
}

func playbookFailedRunHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	limit := req.GetInt("limit", 20)

	runs, err := db.GetRecentPlaybookRuns(ctx, limit, models.PlaybookRunStatusFailed)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	jsonData, err := json.Marshal(runs)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(jsonData)), nil

}

func playbookRunHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	playbookToolName := req.Params.Name
	playbookID := currentPlaybookTools[playbookToolName]
	if playbookID == "" {
		return mcp.NewToolResultError(fmt.Sprintf("tool[%s] is not associated with any playbok", playbookToolName)), nil
	}

	pb, err := query.FindPlaybook(ctx, playbookID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if pb == nil {
		return mcp.NewToolResultError(fmt.Sprintf("playbook[%s] not found", playbookID)), nil
	}

	params := req.GetArguments()
	pj, err := json.Marshal(params)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	var rp playbook.RunParams
	if err := json.Unmarshal(pj, &rp); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	run, err := playbook.Run(ctx, pb, rp)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	jsonData, err := json.Marshal(run)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(jsonData)), nil
}

func playbookListToolHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	jsonData, err := json.Marshal(slices.Collect(maps.Keys(currentPlaybookTools)))
	if err != nil {
		return mcp.NewToolResultError(err.Error()), err
	}
	return mcp.NewToolResultText(string(jsonData)), nil
}

func playbookResourceHandler(goctx gocontext.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return nil, err
	}

	id := extractID(req.Params.URI)
	pb, err := query.FindPlaybook(ctx, id)
	if err != nil {
		return nil, err
	}
	if pb == nil {
		return nil, fmt.Errorf("playbook[%s] not found", id)
	}
	jsonData, err := json.Marshal(pb)
	if err != nil {
		return nil, err
	}

	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      req.Params.URI,
			MIMEType: "application/json",
			Text:     string(jsonData),
		},
	}, nil
}

// ToolName -> Playbook ID
var currentPlaybookTools = make(map[string]string)

func syncPlaybooksAsTools(ctx context.Context, s *server.MCPServer) error {
	playbooks, err := gorm.G[models.Playbook](ctx.DB()).Where("deleted_at IS NULL").Find(ctx)
	if err != nil {
		return fmt.Errorf("error fetching playbooks: %w", err)
	}
	var newPlaybookTools []string
	for _, pb := range playbooks {
		var spec v1.PlaybookSpec
		if err := json.Unmarshal(pb.Spec, &spec); err != nil {
			return fmt.Errorf("error unmarshaling playbook[%s] spec: %w", pb.ID, err)
		}

		root := &jsonschema.Schema{
			Type:       "object",
			Required:   []string{"id"},
			Properties: orderedmap.New[string, *jsonschema.Schema](),
		}

		root.Properties.Set("agent_id", &jsonschema.Schema{Type: "string", Description: "UUID of agent to run playbook on. Leave empty if not specified"})
		if len(spec.Checks) > 0 {
			root.Properties.Set("check_id", &jsonschema.Schema{Type: "string", Description: "UUID of the health check this playbook belongs to"})
			root.Required = append(root.Required, "check_id")
		}
		if len(spec.Configs) > 0 {
			root.Properties.Set("config_id", &jsonschema.Schema{Type: "string", Description: "UUID of config_item/catalog_item this playbook belongs to"})
			root.Required = append(root.Required, "config_id")
		}
		if len(spec.Components) > 0 {
			root.Properties.Set("component_id", &jsonschema.Schema{Type: "string", Description: "UUID of component this playbook belongs to"})
			root.Required = append(root.Required, "component_id")
		}

		paramsSchema := &jsonschema.Schema{
			Type:       "object",
			Properties: orderedmap.New[string, *jsonschema.Schema](),
		}
		var requiredParams []string
		for _, param := range spec.Parameters {
			s := &jsonschema.Schema{Type: "string", Description: param.Description}
			// RunParams only has support for strings so we use enum in these cases
			if param.Type == v1.PlaybookParameterTypeCheckbox {
				s.Enum = []any{"true", "false"}
				s.Default = "false"
				s.Description += ". This is a boolean field, either true or false as strings."
			}
			paramsSchema.Properties.Set(param.Name, s)
			if param.Required {
				requiredParams = append(requiredParams, param.Name)
			}
		}
		paramsSchema.Required = requiredParams
		root.Properties.Set("params", paramsSchema)

		rj, err := root.MarshalJSON()
		if err != nil {
			return fmt.Errorf("error marshalling root json schema: %w", err)
		}

		toolName := generatePlaybookToolName(pb)
		s.AddTool(mcp.NewToolWithRawSchema(toolName, pb.Description, rj), playbookRunHandler)
		newPlaybookTools = append(newPlaybookTools, toolName)
		currentPlaybookTools[toolName] = pb.GetID()
	}

	// Delete old playbooks and update currentPlaybookTools list
	currentToolNames := slices.Collect(maps.Keys(currentPlaybookTools))
	_, playbookToolsToDelete := lo.Difference(newPlaybookTools, currentToolNames)
	s.DeleteTools(playbookToolsToDelete...)
	// Remove from currentPlaybookTools
	maps.DeleteFunc(currentPlaybookTools, func(k, v string) bool {
		return slices.Contains(playbookToolsToDelete, k)
	})

	return nil
}

func generatePlaybookToolName(pb models.Playbook) string {
	name := strings.ToLower(strings.ReplaceAll(lo.CoalesceOrEmpty(pb.Title, pb.Name), " ", "-"))
	toolName := strings.ToLower(fmt.Sprintf("%s_%s_%s", name, pb.Namespace, pb.Category))
	return fixMCPToolNameIfRequired(toolName)
}

func registerPlaybook(ctx context.Context, s *server.MCPServer) {
	s.AddResourceTemplate(
		mcp.NewResourceTemplate("playbook://{id}", "Playbook",
			mcp.WithTemplateDescription("Playbook data"), mcp.WithTemplateMIMEType(echo.MIMEApplicationJSON)),
		playbookResourceHandler,
	)

	s.AddTool(mcp.NewTool("playbooks_list_all",
		mcp.WithDescription("List all available playbooks")), playbookListToolHandler)

	playbookRecentRunTool := mcp.NewTool("playbook_recent_runs",
		mcp.WithDescription("Playbook recent runs"),
		mcp.WithNumber("limit",
			mcp.Required(),
			mcp.Description("Search query"),
		))

	s.AddTool(playbookRecentRunTool, playbookRecentRunHandler)

	playbookFailedRunTool := mcp.NewTool("playbook_failed_runs",
		mcp.WithDescription("Playbook recent runs"),
		mcp.WithNumber("limit",
			mcp.Required(),
			mcp.Description("Search query"),
		))

	s.AddTool(playbookFailedRunTool, playbookFailedRunHandler)

	// Periodically call sync to handle new playbooks added
	go func() {
		for {
			if err := syncPlaybooksAsTools(ctx, s); err != nil {
				logger.Fatalf("error adding playbooks as mcp tool: %w", err)
			}

			time.Sleep(1 * time.Hour)
		}
	}()
}
