package mcp

import (
	gocontext "context"
	"fmt"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/db"
	icrbac "github.com/flanksource/incident-commander/rbac"
)

const (
	toolResolveExternalUser  = "resolve_external_user"
	toolResolveExternalGroup = "resolve_external_group"
	toolResolveConfig        = "resolve_config"
)

func resolveExternalUserHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := requireCatalogRead(ctx); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	q, err := req.RequireString("query")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	limit := req.GetInt("limit", 10)

	var result any
	err = auth.WithRLS(ctx, func(rlsCtx context.Context) error {
		users, err := db.ResolveExternalUsers(rlsCtx, q, limit)
		result = users
		return err
	})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	return structToMCPResponse(req, result), nil
}

func resolveExternalGroupHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := requireCatalogRead(ctx); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	q, err := req.RequireString("query")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	limit := req.GetInt("limit", 10)

	var result any
	err = auth.WithRLS(ctx, func(rlsCtx context.Context) error {
		groups, err := db.ResolveExternalGroups(rlsCtx, q, limit)
		result = groups
		return err
	})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	return structToMCPResponse(req, result), nil
}

func resolveConfigHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	q, err := req.RequireString("query")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	configType := req.GetString("type", "")
	limit := req.GetInt("limit", 10)

	var result any
	err = auth.WithRLS(ctx, func(rlsCtx context.Context) error {
		items, err := db.ResolveConfigItems(rlsCtx, q, configType, limit)
		result = items
		return err
	})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	return structToMCPResponse(req, result), nil
}

// requireCatalogRead checks read on the whole catalog, which the /db API requires for external users and groups.
// Those tables have no row-level security, so they're served only to a caller who may read every config.
func requireCatalogRead(ctx context.Context) error {
	if !icrbac.CheckContext(ctx, policy.ObjectCatalog, policy.ActionRead) {
		return fmt.Errorf("forbidden: %s does not have %s permission on %s", ctx.Subject(), policy.ActionRead, policy.ObjectCatalog)
	}
	return nil
}

func registerResolve(s *server.MCPServer) {
	s.AddTool(mcp.NewTool(toolResolveExternalUser,
		mcp.WithDescription("Resolve an external user by ID, name, or email to get their UUID and details. Use this to look up user identifiers before calling access audit tools like search_catalog_access_log."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("User UUID, name, or email to search for. Partial matches supported for name and email."),
		),
		mcp.WithNumber("limit",
			mcp.Description(fmt.Sprintf("Max results to return (default: %d)", 10)),
		),
	), resolveExternalUserHandler)

	s.AddTool(mcp.NewTool(toolResolveExternalGroup,
		mcp.WithDescription("Resolve an external group by ID or name to get their UUID and details. Use this to look up group identifiers for access audit queries."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("Group UUID or name to search for. Partial matches supported for name."),
		),
		mcp.WithNumber("limit",
			mcp.Description(fmt.Sprintf("Max results to return (default: %d)", 10)),
		),
	), resolveExternalGroupHandler)

	s.AddTool(mcp.NewTool(toolResolveConfig,
		mcp.WithDescription("Resolve a config item by ID or name to get its UUID. Use this when you have a config name and need its ID for other tools like search_catalog_access_log or describe_catalog."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("Config item UUID or name to search for. Partial matches supported for name."),
		),
		mcp.WithString("type",
			mcp.Description("Optional config type filter (e.g. 'Kubernetes::Deployment', 'AWS::EC2::Instance')"),
		),
		mcp.WithNumber("limit",
			mcp.Description(fmt.Sprintf("Max results to return (default: %d)", 10)),
		),
	), resolveConfigHandler)
}
