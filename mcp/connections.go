package mcp

import (
	gocontext "context"
	"encoding/json"
	"fmt"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/incident-commander/db"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/samber/lo"
)

func ConnectionListHandler(goctx gocontext.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return nil, err
	}

	conns, err := db.ListConnections(ctx)
	if err != nil {
		return newToolResultError(err.Error()), nil
	}
	return structToMCPResponse(conns), nil
}

func ConnectionResourceHandler(goctx gocontext.Context, req *mcp.ReadResourceRequest) ([]*mcp.ResourceContents, error) {
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

	jsonData, err := json.Marshal(conn)
	if err != nil {
		return nil, err
	}

	return []*mcp.ResourceContents{
		{
			URI:      req.Params.URI,
			MIMEType: "application/json"),
			Text:     lo.ToPtr(string(jsonData)),
		},
	}, nil
}

func registerConnections(s *mcp.Server) {
	s.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: "connection://{namespace}/{name}",
		Name:        "Config Item",
		Description: "Config Item Data"),
		MIMEType:    lo.ToPtr("application/json"),
	}, ConnectionResourceHandler)

	s.AddTool(&mcp.Tool{
		Name:        "list_connections",
		Description: "List all connection endpoints and credentials. Returns empty array if no connections configured. Use for discovering available data sources."),
	}, ConnectionListHandler)
}
