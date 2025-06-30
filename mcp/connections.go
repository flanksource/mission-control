package mcp

import (
	gocontext "context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/flanksource/duty"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/incident-commander/db"
	"github.com/labstack/echo/v4"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func ConnectionListHandler(goctx gocontext.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	ctx, _, err := duty.Start("mission-control-2", duty.ClientOnly)
	if err != nil {
		return nil, err
	}

	conns, err := db.ListConnections(ctx)
	if err != nil {
		return nil, err
	}

	var contents []mcp.ResourceContents
	for _, c := range conns {
		jsonData, err := json.Marshal(conns)
		if err != nil {
			return nil, err
		}
		contents = append(contents, mcp.TextResourceContents{
			URI:      fmt.Sprintf("connection://%s/%s", c.Namespace, c.Name),
			MIMEType: "application/json",
			Text:     string(jsonData),
		})

	}

	return contents, nil
}

func ConnectionResourceHandler(goctx gocontext.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	ctx, _, err := duty.Start("mission-control-2", duty.ClientOnly)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(strings.TrimPrefix(req.Params.URI, "connection://"), "/")
	if len(parts) != 2 {
		return nil, fmt.Errorf("")
	}

	namespace, name := parts[1], parts[0]

	conns, err := context.FindConnection(ctx, name, namespace)
	if err != nil {
		return nil, err
	}

	jsonData, err := json.Marshal(conns)
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

	s.AddResource(mcp.NewResource("connection://list", "Config Types",
		mcp.WithResourceDescription("List all config types"), mcp.WithMIMEType(echo.MIMEApplicationJSON)),
		ConnectionListHandler)
}
