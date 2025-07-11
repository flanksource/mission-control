package mcp

import (
	gocontext "context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/incident-commander/db"
	"github.com/labstack/echo/v4"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func ConnectionListHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return nil, err
	}

	conns, err := db.ListConnections(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	jsonData, err := json.Marshal(conns)
	if err != nil {
		return nil, err
	}

	return mcp.NewToolResultText(string(jsonData)), nil
}

func ConnectionResourceHandler(goctx gocontext.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(strings.TrimPrefix(req.Params.URI, "connection://"), "/")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid connection format: %s", req.Params.URI)
	}

	namespace, name := parts[0], parts[1]

	conn, err := context.FindConnection(ctx, name, namespace)
	if err != nil {
		return nil, err
	}

	if conn == nil {
		return nil, fmt.Errorf("connection not found")
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
		mcp.NewResourceTemplate("connection://{namespace}/{name}", "Config Item",
			mcp.WithTemplateDescription("Config Item Data"), mcp.WithTemplateMIMEType(echo.MIMEApplicationJSON)),
		ConnectionResourceHandler)

	s.AddTool(mcp.NewTool("list_connections", mcp.WithDescription("List all connections")), ConnectionListHandler)
}
