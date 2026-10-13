package mcp

import (
	gocontext "context"
	"encoding/json"
	"fmt"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/rbac"
	"github.com/labstack/echo/v4"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const toolListConnections = "list_connections"

func ConnectionListHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return nil, err
	}

	if !rbac.CheckContext(ctx, policy.ObjectConnection, policy.ActionRead) {
		return mcp.NewToolResultError("read permission required on connections"), nil
	}
	conns, err := db.ListConnections(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	visible := conns[:0]
	for _, conn := range conns {
		if rbac.AuthorizeConnection(ctx, conn, policy.ActionRead) == nil {
			visible = append(visible, conn)
		}
	}
	return structToMCPResponse(req, visible), nil
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

	conn, err := context.FindConnection(ctx, name, namespace)
	if err != nil {
		return nil, err
	}

	if conn == nil {
		return nil, fmt.Errorf("connection not found")
	}
	if err := rbac.AuthorizeConnection(ctx, *conn, policy.ActionRead); err != nil {
		return nil, err
	}

	jsonData, err := json.Marshal(db.MaskConnection(*conn))
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

	s.AddTool(mcp.NewTool(toolListConnections,
		mcp.WithDescription("List all connection endpoints, with their credentials masked. Returns empty array if no connections configured. Use for discovering available data sources."),
		mcp.WithReadOnlyHintAnnotation(true)), ConnectionListHandler)
}
