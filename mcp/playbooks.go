package mcp

import (
	gocontext "context"
	"encoding/json"
	"strings"

	"github.com/flanksource/duty"
	//"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/playbook"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func playbookRecentRunHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, _, err := duty.Start("mission-control-2", duty.ClientOnly)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	limit := req.GetInt("limit", 20)

	pbrs, err := db.GetRecentPlaybookRuns(ctx, limit)
	jsonData, err := json.Marshal(pbrs)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	return mcp.NewToolResultText(string(jsonData)), nil

}

func playbookFailedRunHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, _, err := duty.Start("mission-control-2", duty.ClientOnly)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	limit := req.GetInt("limit", 20)

	runs, err := db.GetRecentPlaybookRuns(ctx, limit, models.PlaybookRunStatusFailed)
	jsonData, err := json.Marshal(runs)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(jsonData)), nil

}

func playbookRunHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, _, err := duty.Start("mission-control-2", duty.ClientOnly)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	playbookID := req.GetString("id", "")
	params := req.GetArguments()["params"]

	pb, err := query.FindPlaybook(ctx, playbookID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	pj, err := json.Marshal(params)

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

func registerPlaybook(s *server.MCPServer) {
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

	playbookRunTool := mcp.NewTool("playbook_exec_run",
		mcp.WithDescription("Playbook execute run"),
		mcp.WithString("name",
			mcp.Required(),
			mcp.Description("Search query"),
		),
		mcp.WithObject("params", mcp.Required()))

	s.AddTool(playbookRunTool, playbookRunHandler)
}

func extractID(uri string) string {
	// Extract ID from "users://123" format
	parts := strings.Split(uri, "://")
	if len(parts) == 2 {
		return parts[1]
	}
	return ""
}
