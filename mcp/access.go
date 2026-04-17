package mcp

import (
	gocontext "context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/db"
)

const (
	toolSearchCatalogAccessMapping = "search_catalog_access_mapping"
	toolSearchCatalogAccessLog     = "search_catalog_access_log"
	toolSearchCatalogAccessReviews = "search_catalog_access_reviews"
)

func searchCatalogAccessMappingHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	q, err := req.RequireString("query")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	limit := req.GetInt("limit", defaultQueryLimit)

	var rows []db.RBACAccessRow
	err = auth.WithRLS(ctx, func(rlsCtx context.Context) error {
		rows, err = db.GetRBACAccess(rlsCtx, []types.ResourceSelector{{Search: q}}, false)
		return err
	})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if limit <= 0 {
		limit = defaultQueryLimit
	}
	if len(rows) > limit {
		rows = rows[:limit]
	}

	return structToMCPResponse(req, rows), nil
}

func searchCatalogAccessLogHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	rawID, err := req.RequireString("config_id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	configID, err := uuid.Parse(rawID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid config_id: %v", err)), nil
	}

	var userID *uuid.UUID
	if rawUserID := req.GetString("user_id", ""); rawUserID != "" {
		id, err := uuid.Parse(rawUserID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid user_id: %v", err)), nil
		}
		userID = &id
	}

	limit := req.GetInt("limit", 50)

	var rows []db.AccessLogRow
	err = auth.WithRLS(ctx, func(rlsCtx context.Context) error {
		rows, err = db.GetAccessLogs(rlsCtx, configID, userID, limit)
		return err
	})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	return structToMCPResponse(req, rows), nil
}

func searchCatalogAccessReviewsHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	var configID *uuid.UUID
	if rawID := req.GetString("config_id", ""); rawID != "" {
		id, err := uuid.Parse(rawID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid config_id: %v", err)), nil
		}
		configID = &id
	}

	sinceStr := req.GetString("since", "90d")
	sinceDuration, err := parseDuration(sinceStr)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid since duration: %v", err)), nil
	}
	sinceTime := time.Now().Add(-sinceDuration)

	limit := req.GetInt("limit", 50)

	var rows []db.AccessReviewRow
	err = auth.WithRLS(ctx, func(rlsCtx context.Context) error {
		rows, err = db.GetAccessReviews(rlsCtx, configID, sinceTime, limit)
		return err
	})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	return structToMCPResponse(req, rows), nil
}

func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 90 * 24 * time.Hour, nil
	}

	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}

	if len(s) < 2 {
		return 0, fmt.Errorf("invalid duration: %s", s)
	}

	unit := s[len(s)-1]
	numStr := s[:len(s)-1]
	n, err := strconv.Atoi(numStr)
	if err != nil {
		return 0, fmt.Errorf("invalid duration: %s", s)
	}

	switch unit {
	case 'd':
		return time.Duration(n) * 24 * time.Hour, nil
	case 'w':
		return time.Duration(n) * 7 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("unsupported duration unit: %c (use d, w, h, m, s)", unit)
	}
}

func registerAccess(s *server.MCPServer) {
	s.AddTool(mcp.NewTool(toolSearchCatalogAccessMapping,
		mcp.WithDescription("Search the current access state and RBAC mappings for infrastructure resources to audit who currently holds permissions. Accepts a flexible search query (e.g. type=Kubernetes::*, name=my-app) to answer questions like 'who has access to this app?', 'list all users with access to Kubernetes deployments', or 'find stale access and overdue reviews'. Returns config item name/type, user email/type, assigned role, group name, and timestamps for last sign-in and last review."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("Search query to filter config items by name or type (e.g. 'type=Kubernetes::*', 'name=my-app')"),
		),
		mcp.WithNumber("limit",
			mcp.Description(fmt.Sprintf("Max results to return (default: %d)", defaultQueryLimit)),
		),
	), searchCatalogAccessMappingHandler)

	s.AddTool(mcp.NewTool(toolSearchCatalogAccessLog,
		mcp.WithDescription("Search historical sign-in and access activity logs for a specific infrastructure configuration item. Investigate security events and answer questions like 'when was this resource last accessed?', 'who has been logging into this system?', or 'was MFA used during access?'. Returns a chronological log with user name/email, MFA status, total access count, and activity timestamp."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithString("config_id",
			mcp.Required(),
			mcp.Description("Config item ID (UUID)"),
		),
		mcp.WithString("user_id",
			mcp.Description("External user ID (UUID) to filter logs by. Use resolve_external_user to find user IDs by name or email."),
		),
		mcp.WithNumber("limit",
			mcp.Description("Max results to return (default: 50)"),
		),
	), searchCatalogAccessLogHandler)

	s.AddTool(mcp.NewTool(toolSearchCatalogAccessReviews,
		mcp.WithDescription("Search historical access review and certification events to verify when user permissions were last audited or validated. Answers compliance questions like 'when was access to this resource last reviewed?', 'which resources haven't been reviewed recently?', or 'who performed the last access review?'. Returns config item details, reviewed user/role, review source, and certification timestamp."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithString("config_id",
			mcp.Description("Config item ID (UUID). If omitted, searches across all configs."),
		),
		mcp.WithString("since",
			mcp.Description("How far back to search. Duration string like '90d', '30d', '7d', '24h' (default: '90d')"),
		),
		mcp.WithNumber("limit",
			mcp.Description("Max results to return (default: 50)"),
		),
	), searchCatalogAccessReviewsHandler)
}
