package mcp

import (
	gocontext "context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

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
	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/playbook"
)

// playbookToolNameToID maps tool names to playbook IDs
var playbookToolNameToID = sync.Map{}

const (
	toolGetPlaybookRunSteps   = "get_playbook_run_steps"
	toolGetFailedPlaybookRuns = "get_playbook_failed_runs"
	toolGetRecentPlaybookRuns = "get_playbook_recent_runs"
	toolGetAllPlaybooks       = "get_all_playbooks"
)

func addPlaybooksAsTool(goctx gocontext.Context, srv *server.MCPServer, session server.ClientSession) error {
	sessionID := session.SessionID()

	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return ctx.Oops().Wrapf(err, "failed to get duty context for session %s", sessionID)
	}

	var playbooks []models.Playbook
	err = auth.WithRLS(ctx, func(txCtx context.Context) error {
		var err error
		playbooks, err = gorm.G[models.Playbook](txCtx.DB()).Where("deleted_at IS NULL").Find(txCtx)
		return err
	})
	if err != nil {
		return ctx.Oops().Wrapf(err, "failed to get playbook tools for session %s", sessionID)
	}

	tools, err := getPlaybooksAsTools(playbooks)
	if err != nil {
		return ctx.Oops().Wrapf(err, "failed to convert playbooks to tools for session %s", sessionID)
	}

	sessionTools := lo.Map(tools, func(tool mcp.Tool, _ int) server.ServerTool {
		return server.ServerTool{
			Tool:    tool,
			Handler: playbookRunHandler,
		}
	})
	if err := srv.AddSessionTools(sessionID, sessionTools...); err != nil {
		return ctx.Oops().Wrapf(err, "failed to add tool %d to session %s", len(sessionTools), sessionID)
	}

	ctx.Logger.Infof("Successfully registered %d playbook tools for session %q", len(tools), sessionID)
	return nil
}

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

	var pbrs []models.PlaybookRun
	err = auth.WithRLS(ctx, func(rlsCtx context.Context) error {
		pbrs, err = db.GetRecentPlaybookRuns(rlsCtx, limit, playbookID)
		return err
	})

	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return structToMCPResponse(req, pbrs), nil
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

	var runs []models.PlaybookRun
	err = auth.WithRLS(ctx, func(rlsCtx context.Context) error {
		runs, err = db.GetRecentPlaybookRuns(rlsCtx, limit, playbookID, models.PlaybookRunStatusFailed)
		return err
	})

	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return structToMCPResponse(req, runs), nil
}

func playbookRunHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	toolName := req.Params.Name
	playbookIDRaw, ok := playbookToolNameToID.Load(toolName)
	if !ok {
		return mcp.NewToolResultError(fmt.Sprintf("playbook tool %s not found", toolName)), nil
	}
	playbookID := playbookIDRaw.(string)

	var pb *models.Playbook
	err = auth.WithRLS(ctx, func(rlsCtx context.Context) error {
		pb, err = query.FindPlaybook(rlsCtx, playbookID)
		if err != nil {
			return err
		} else if pb == nil {
			return fmt.Errorf("playbook[%s] not found", playbookID)
		}

		return nil
	})

	params := req.GetArguments()
	pj, err := json.Marshal(params)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if params["agent_id"] == "" {
		// if the MCP client doesn't care about the agent, we need to delete this field
		// because unmarshalling to RunParams fail (empty string cannot be unmarshalled to uuid)
		delete(params, "agent_id")
	}

	var rp playbook.RunParams
	if err := json.Unmarshal(pj, &rp); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	run, err := playbook.Run(ctx, pb, rp)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	response := fmt.Sprintf("Started a new run: %s for the playbook: %s/%s\n", run.ID, pb.ID, pb.NamespacedName())
	response += fmt.Sprintf("Use the %s tool to get the run steps.", toolGetPlaybookRunSteps)
	return mcp.NewToolResultText(response), nil
}

func playbookListToolHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	type playbookInfo struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}

	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	var playbooks []models.Playbook
	err = auth.WithRLS(ctx, func(txCtx context.Context) error {
		var err error
		playbooks, err = gorm.G[models.Playbook](txCtx.DB()).Select("id", "name", "title", "namespace", "category").Where("deleted_at IS NULL").Find(txCtx)
		return err
	})
	if err != nil {
		return mcp.NewToolResultError(ctx.Oops().Wrapf(err, "Failed to get playbook tools").Error()), nil
	}

	response := lo.Map(playbooks, func(pb models.Playbook, _ int) playbookInfo {
		return playbookInfo{
			ID:   pb.ID.String(),
			Name: generatePlaybookToolName(pb),
		}
	})

	return structToMCPResponse(req, response), nil
}

// PlaybookRunActionDetail represents the response from get_playbook_run_actions SQL function
// It extends the base PlaybookRunAction with additional fields from the SQL query
type PlaybookRunActionDetail struct {
	models.PlaybookRunAction
	Agent     types.JSONMap `json:"agent,omitempty"`
	Artifacts types.JSON    `json:"artifacts,omitempty"`
}

func getPlaybookRunStepsHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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

	var actions []PlaybookRunActionDetail
	err = auth.WithRLS(ctx, func(rlsCtx context.Context) error {
		query := fmt.Sprintf("SELECT %s FROM get_playbook_run_actions(?)", selectFields)
		return rlsCtx.DB().Raw(query, runID).Scan(&actions).Error
	})

	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("error fetching playbook run details: %v", err)), nil
	}

	return structToMCPResponse(req, actions), nil
}

func playbookResourceHandler(goctx gocontext.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return nil, err
	}

	id := extractID(req.Params.URI)

	var pb *models.Playbook
	err = auth.WithRLS(ctx, func(rlsCtx context.Context) error {
		pb, err = query.FindPlaybook(rlsCtx, id)
		return err
	})

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

func getPlaybooksAsTools(playbooks []models.Playbook) ([]mcp.Tool, error) {
	var newPlaybookTools []mcp.Tool
	for _, pb := range playbooks {
		var spec v1.PlaybookSpec
		if err := json.Unmarshal(pb.Spec, &spec); err != nil {
			return nil, fmt.Errorf("error unmarshaling playbook[%s] spec: %w", pb.ID, err)
		}

		root := &jsonschema.Schema{
			Type:       "object",
			Properties: orderedmap.New[string, *jsonschema.Schema](),
		}

		root.Properties.Set("agent_id", &jsonschema.Schema{
			Type:        "string",
			Format:      "uuid",
			Description: "UUID of agent to run playbook on. Optional - leave empty if not specified",
			Extras:      map[string]any{"nullable": true},
		})

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
			return nil, fmt.Errorf("error marshaling root json schema: %w", err)
		}

		toolName := generatePlaybookToolName(pb)
		playbookToolNameToID.Store(toolName, pb.ID.String())

		t := mcp.NewToolWithRawSchema(toolName, pb.Description, rj)
		t.Annotations.Title = pb.Name
		newPlaybookTools = append(newPlaybookTools, t)
	}

	return newPlaybookTools, nil
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

	s.AddTool(mcp.NewTool(toolGetAllPlaybooks,
		mcp.WithDescription("List all available playbooks")), playbookListToolHandler)

	playbookRecentRunTool := mcp.NewTool(toolGetRecentPlaybookRuns,
		mcp.WithDescription("Get recent playbook execution history as JSON array. Each entry contains run details, status, timing, and results."),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of recent runs to return (default: 20)"),
		),
		mcp.WithString("playbook_id",
			mcp.Description("Optional UUID of the playbook to filter runs by. If not provided, returns runs for all playbooks."),
		))

	s.AddTool(playbookRecentRunTool, playbookRecentRunHandler)

	playbookFailedRunTool := mcp.NewTool(toolGetFailedPlaybookRuns,
		mcp.WithDescription("Get recent failed playbook runs as JSON array. Each entry contains failure details, error messages, and timing information."),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of failed runs to return (default: 20)"),
		),
		mcp.WithString("playbook_id",
			mcp.Description("Optional UUID of the playbook to filter runs by. If not provided, returns failed runs for all playbooks."),
		))

	s.AddTool(playbookFailedRunTool, playbookFailedRunsHandler)

	playbookRunStepsTool := mcp.NewTool(toolGetPlaybookRunSteps,
		mcp.WithDescription("Get detailed information about a playbook run including all actions. Returns actions from both the run and any child runs. Actions are ordered by start time."),
		mcp.WithString("run_id",
			mcp.Required(),
			mcp.Description("The UUID of the playbook run to get details for"),
		),
		mcp.WithBoolean("withResult",
			mcp.Description("Include the result field from actions. Set to false to reduce response size (default: true)"),
		),
		mcp.WithReadOnlyHintAnnotation(true))

	s.AddTool(playbookRunStepsTool, getPlaybookRunStepsHandler)
}
