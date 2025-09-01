package mcp

import (
	gocontext "context"
	"encoding/json"

	"github.com/flanksource/incident-commander/views"
	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type viewSummary struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Namespace string    `json:"namespace"`
	Title     string    `json:"title"`
}

func listViewHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return nil, err
	}

	var views []viewSummary
	err = ctx.DB().Select("id", "name", "namespace", "title").Table("views_summary").Scan(&views).Error
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	jsonData, err := json.Marshal(views)
	if err != nil {
		return nil, err
	}

	return mcp.NewToolResultText(string(jsonData)), nil
}

func getViewHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return nil, err
	}

	namespace, err := req.RequireString("namespace")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	response, err := views.ReadOrPopulateViewTable(ctx, namespace, name)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	jsonData, err := json.Marshal(response)
	if err != nil {
		return nil, err
	}

	return mcp.NewToolResultText(string(jsonData)), nil
}

func registerViews(s *server.MCPServer) {
	s.AddTool(mcp.NewTool("list_views", mcp.WithDescription("List all available dashboard views with their namespace, name, and title")), listViewHandler)

	s.AddTool(mcp.NewTool("get_view",
		mcp.WithDescription(`Retrieve dashboard view data containing business metrics and operational data.
		Views include tabular data (display as markdown tables) and chart panels (show panel data if visualization unavailable).
		Use list_views first to discover available views by their namespace and name.`),
		mcp.WithString("namespace", mcp.Description("Namespace of the view")),
		mcp.WithString("name", mcp.Description("Name of the view")),
	), getViewHandler)
}
