package mcp

import (
	gocontext "context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"gorm.io/gorm"

	"github.com/flanksource/incident-commander/connection"
)

const (
	toolListConnections = "list_connections"
	toolTestConnection  = "test_connection"
)

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

func testConnectionHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	ref, err := req.RequireString("connection")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	query := ctx.DB().Where("deleted_at IS NULL")
	if _, err := uuid.Parse(ref); err == nil {
		query = query.Where("id = ?", ref)
	} else if namespace, name, ok := strings.Cut(strings.TrimPrefix(ref, "connection://"), "/"); ok {
		query = query.Where("namespace = ? AND name = ?", namespace, name)
	} else {
		return mcp.NewToolResultError(fmt.Sprintf("connection %q must be a UUID or namespace/name", ref)), nil
	}

	var conn models.Connection
	if err := query.First(&conn).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return mcp.NewToolResultError(fmt.Sprintf("connection %s not found", ref)), nil
		}
		return mcp.NewToolResultError(err.Error()), nil
	}

	// Only whether it works is returned: the test's payload, token and requests can hold credentials
	if _, err := connection.TestAs(ctx, conn); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("connection %s/%s failed: %v", conn.Namespace, conn.Name, err)), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("connection %s/%s works", conn.Namespace, conn.Name)), nil
}

func registerConnections(s *server.MCPServer) {
	s.AddResourceTemplate(
		mcp.NewResourceTemplate("connection://{namespace}/{name}", "Connection",
			mcp.WithTemplateDescription("A connection's id, name, namespace, type and source"), mcp.WithTemplateMIMEType(echo.MIMEApplicationJSON)),
		ConnectionResourceHandler)

	s.AddTool(mcp.NewTool(toolListConnections,
		mcp.WithDescription("List connections by id, name, namespace, type and source. Settings and credentials aren't returned. Returns empty array if no connections configured. Use for discovering available data sources."),
		mcp.WithReadOnlyHintAnnotation(true)), ConnectionListHandler)

	s.AddTool(mcp.NewTool(toolTestConnection,
		mcp.WithDescription("Test whether a connection works, by connecting with its credentials. Returns only whether it works and the error if it doesn't."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithString("connection", mcp.Required(), mcp.Description("UUID or namespace/name of the connection")),
	), testConnectionHandler)
}
