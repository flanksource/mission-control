package mcp

import (
	gocontext "context"
	"encoding/json"
	"fmt"

	"github.com/labstack/echo/v4"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/flanksource/incident-commander/connection"
)

const toolListConnections = "list_connections"

func ConnectionListHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return nil, err
	}

	conns, err := connection.ListSummaries(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return structToMCPResponse(req, conns), nil
}

func ConnectionResourceHandler(goctx gocontext.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return nil, err
	}

	namespace, name, err := extractNamespaceName(req.Params.URI)
	if err != nil {
		return nil, fmt.Errorf("invalid connection format: %s", req.Params.URI)
	}

	conn, err := connection.GetSummary(ctx, namespace, name)
	if err != nil {
		return nil, err
	}

	jsonData, err := json.Marshal(conn)
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

func registerConnections(s *server.MCPServer) {
	s.AddResourceTemplate(
		mcp.NewResourceTemplate("connection://{namespace}/{name}", "Connection",
			mcp.WithTemplateDescription("A connection's id, name, namespace, type and source"), mcp.WithTemplateMIMEType(echo.MIMEApplicationJSON)),
		ConnectionResourceHandler)

	s.AddTool(mcp.NewTool(toolListConnections,
		mcp.WithDescription("List connections by id, name, namespace, type and source. Settings and credentials aren't returned. Returns empty array if no connections configured. Use for discovering available data sources."),
		mcp.WithReadOnlyHintAnnotation(true)), ConnectionListHandler)
}
