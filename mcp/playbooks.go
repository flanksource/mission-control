package mcp

import (
	gocontext "context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"github.com/invopop/jsonschema"
	"github.com/labstack/echo/v4"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/samber/lo"
	orderedmap "github.com/wk8/go-ordered-map/v2"
	"gorm.io/gorm"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/playbook"
)

func playbookRecentRunHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	limit := req.GetInt("limit", 20)

	var playbookID *uuid.UUID
	if playbookIDStr := req.GetString("playbook_id", ""); playbookIDStr != "" {
		parsed, err := uuid.Parse(playbookIDStr)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid playbook_id: %v", err)), nil
		}
		playbookID = &parsed
	}

	pbrs, err := db.GetRecentPlaybookRuns(ctx, limit, playbookID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return structToMCPResponse(pbrs), nil
}

func playbookFailedRunsHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	limit := req.GetInt("limit", 20)

	var playbookID *uuid.UUID
	if playbookIDStr := req.GetString("playbook_id", ""); playbookIDStr != "" {
		parsed, err := uuid.Parse(playbookIDStr)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid playbook_id: %v", err)), nil
		}
		playbookID = &parsed
	}

	runs, err := db.GetRecentPlaybookRuns(ctx, limit, playbookID, models.PlaybookRunStatusFailed)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return structToMCPResponse(runs), nil
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
	return structToMCPResponse(run), nil
}

func playbookListToolHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	type playbookInfo struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}

	playbooks := make([]playbookInfo, 0, len(currentPlaybookTools))
	for toolName, playbookID := range currentPlaybookTools {
		playbooks = append(playbooks, playbookInfo{
			ID:   playbookID,
			Name: toolName,
		})
	}

	return structToMCPResponse(playbooks), nil
}

// PlaybookRunActionDetail represents the response from get_playbook_run_actions SQL function
// It extends the base PlaybookRunAction with additional fields from the SQL query
type PlaybookRunActionDetail struct {
	models.PlaybookRunAction
	Agent     types.JSONMap `json:"agent,omitempty"`
	Artifacts types.JSON    `json:"artifacts,omitempty"`
}

func getPlaybookRunDetailsHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	runID, err := req.RequireString("run_id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	withResult := req.GetBool("withResult", true)

	selectFields := "*"
	if !withResult {
		// Select only the fields we need
		selectFields = "id, name, playbook_run_id, status, scheduled_time, start_time, end_time, agent_id, retry_count, agent, artifacts"
	}

	query := fmt.Sprintf("SELECT %s FROM get_playbook_run_actions(?)", selectFields)
	var actions []PlaybookRunActionDetail
	if err := ctx.DB().Raw(query, runID).Scan(&actions).Error; err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("error fetching playbook run details: %v", err)), nil
	}

	return structToMCPResponse(actions), nil
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
			root.Properties.Set("config_id", &jsonschema.Schema{Type: "string", Description: "UUID of config item (from search_catalog results). Example: f47ac10b-58cc-4372-a567-0e02b2c3d479"})
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
			return fmt.Errorf("error marshaling root json schema: %w", err)
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

func registerPlaybook(s *server.MCPServer) {
	s.AddResourceTemplate(
		mcp.NewResourceTemplate("playbook://{idOrName}", "Playbook",
			mcp.WithTemplateDescription("Playbook data"), mcp.WithTemplateMIMEType(echo.MIMEApplicationJSON)),
		playbookResourceHandler,
	)

	s.AddTool(mcp.NewTool("playbooks_list_all",
		mcp.WithDescription("List all available playbooks")), playbookListToolHandler)

	playbookRecentRunTool := mcp.NewTool("playbook_recent_runs",
		mcp.WithDescription("Get recent playbook execution history as JSON array. Each entry contains run details, status, timing, and results."),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of recent runs to return (default: 20)"),
		),
		mcp.WithString("playbook_id",
			mcp.Description("Optional UUID of the playbook to filter runs by. If not provided, returns runs for all playbooks."),
		))

	s.AddTool(playbookRecentRunTool, playbookRecentRunHandler)

	playbookFailedRunTool := mcp.NewTool("playbook_failed_runs",
		mcp.WithDescription("Get recent failed playbook runs as JSON array. Each entry contains failure details, error messages, and timing information."),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of failed runs to return (default: 20)"),
		),
		mcp.WithString("playbook_id",
			mcp.Description("Optional UUID of the playbook to filter runs by. If not provided, returns failed runs for all playbooks."),
		))

	s.AddTool(playbookFailedRunTool, playbookFailedRunsHandler)

	playbookRunDetailsTool := mcp.NewTool("get_playbook_run_details",
		mcp.WithDescription("Get detailed information about a playbook run including all actions. Returns actions from both the run and any child runs. Actions are ordered by start time."),
		mcp.WithString("run_id",
			mcp.Required(),
			mcp.Description("The UUID of the playbook run to get details for"),
		),
		mcp.WithBoolean("withResult",
			mcp.Description("Include the result field from actions. Set to false to reduce response size (default: true)"),
		),
		mcp.WithReadOnlyHintAnnotation(true))

	s.AddTool(playbookRunDetailsTool, getPlaybookRunDetailsHandler)
}
