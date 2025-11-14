package mcp

import (
	gocontext "context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/samber/lo"
	"gorm.io/gorm"

	"github.com/flanksource/incident-commander/auth"
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

	var pbrs []models.PlaybookRun
	err = auth.WithRLS(ctx, func(rlsCtx context.Context) error {
		pbrs, err = db.GetRecentPlaybookRuns(rlsCtx, limit, playbookID)
		return err
	})

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

	var runs []models.PlaybookRun
	err = auth.WithRLS(ctx, func(rlsCtx context.Context) error {
		runs, err = db.GetRecentPlaybookRuns(rlsCtx, limit, playbookID, models.PlaybookRunStatusFailed)
		return err
	})

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

	toolName := req.Params.Name
	playbookIDRaw, ok := playbookToolNameToID.Load(toolName)
	if !ok {
		return mcp.NewToolResultError(fmt.Sprintf("playbook tool %s not found", toolName)), nil
	}
	playbookID := playbookIDRaw.(string)

	// For playbook execution, we need RLS to check if user can access the playbook
	// and ensure the run query is also scoped
	var pb *models.Playbook
	var run *models.PlaybookRun
	err = auth.WithRLS(ctx, func(rlsCtx context.Context) error {
		pb, err = query.FindPlaybook(rlsCtx, playbookID)
		if err != nil {
			return err
		}
		if pb == nil {
			return fmt.Errorf("playbook[%s] not found", playbookID)
		}

		params := req.GetArguments()
		pj, err := json.Marshal(params)
		if err != nil {
			return err
		}

		var rp playbook.RunParams
		if err := json.Unmarshal(pj, &rp); err != nil {
			return err
		}

		run, err = playbook.Run(rlsCtx, pb, rp)
		return err
	})

	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return structToMCPResponse(run), nil
}

func playbookListToolHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	type playbookInfo struct {
		ID       string `json:"id"`
		ToolName string `json:"tool_name"`
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
		return nil, ctx.Oops().Wrapf(err, "Failed to get playbook tools")
	}

	response := lo.Map(playbooks, func(pb models.Playbook, _ int) playbookInfo {
		return playbookInfo{
			ID:       pb.ID.String(),
			ToolName: generatePlaybookToolName(pb),
		}
	})

	return structToMCPResponse(response), nil
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

	var actions []PlaybookRunActionDetail
	err = auth.WithRLS(ctx, func(rlsCtx context.Context) error {
		query := fmt.Sprintf("SELECT %s FROM get_playbook_run_actions(?)", selectFields)
		return rlsCtx.DB().Raw(query, runID).Scan(&actions).Error
	})

	if err != nil {
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
