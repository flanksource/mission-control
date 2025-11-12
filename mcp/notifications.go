package mcp

import (
	gocontext "context"
	"fmt"
	"time"

	"github.com/flanksource/duty/models"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/timberio/go-datemath"
)

// Default fields returned by get_notifications_for_resource to minimize token usage
// Body is excluded from the default selection
var defaultNotificationFields = []string{
	"id",
	"status",
	"count",
	"resource_health",
	"resource_status",
	"resource_health_description",
	"first_observed",
	"source_event",
	"created_at",
	"resource_id",
	"notification_id",
}

func getNotificationDetailHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	sendID, err := req.RequireString("send_id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	var notification models.NotificationSendHistory
	err = ctx.DB().
		Table("notification_send_history_summary").
		Where("id = ?", sendID).
		First(&notification).Error
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to get notification: %v", err)), nil
	}

	return structToMCPResponse([]models.NotificationSendHistory{notification}), nil
}

func getNotificationsForResourceHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	resourceID, err := req.RequireString("resource_id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	limit := req.GetInt("limit", 10)
	since := req.GetString("since", "")
	before := req.GetString("before", "")
	status := req.GetString("status", "")
	selectFields := req.GetStringSlice("select", defaultNotificationFields)

	query := ctx.DB().
		Table("notification_send_history_summary").
		Select(selectFields).
		Where("resource_id = ?", resourceID).
		Order("created_at DESC").
		Limit(limit)

	// Parse and apply since filter (datemath only)
	if since != "" {
		sinceTime, err := parseDateMath(since)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid since parameter: %v", err)), nil
		}
		query = query.Where("created_at >= ?", sinceTime)
	}

	// Parse and apply before filter (datemath only)
	if before != "" {
		beforeTime, err := parseDateMath(before)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid before parameter: %v", err)), nil
		}
		query = query.Where("created_at <= ?", beforeTime)
	}

	// Apply status filter
	if status != "" {
		query = query.Where("status = ?", status)
	}

	var notifications []models.NotificationSendHistory
	err = query.Find(&notifications).Error
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to get notifications: %v", err)), nil
	}

	return structToMCPResponse(notifications), nil
}

// parseDateMath parses a datemath expression and returns the corresponding time
func parseDateMath(val string) (time.Time, error) {
	expr, err := datemath.Parse(val)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid datemath expression '%s': %w", val, err)
	}
	return expr.Time(), nil
}

func registerNotifications(s *server.MCPServer) {
	getNotificationDetailTool := mcp.NewTool("get_notification_detail",
		mcp.WithDescription("Get detailed information about a specific notification including status, body, recipients, resource details, and related entities"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithString("send_id",
			mcp.Required(),
			mcp.Description("UUID of the notification send history record"),
		),
	)
	s.AddTool(getNotificationDetailTool, getNotificationDetailHandler)

	getNotificationsForResourceTool := mcp.NewTool("get_notifications_for_resource",
		mcp.WithDescription("Get notification history for a specific resource (config item, component, check, or canary) with optional time and status filtering. Returns minimal fields by default to save tokens. Use get_notification_detail to get full notification details."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithString("resource_id",
			mcp.Required(),
			mcp.Description("UUID of the resource (from catalog, component, or health check)"),
		),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of notifications to return (default: 10)"),
		),
		mcp.WithString("since",
			mcp.Description("Start time filter using datemath expressions (e.g., 'now-24h', 'now-7d', 'now-30m')"),
		),
		mcp.WithString("before",
			mcp.Description("End time filter using datemath expressions (e.g., 'now-1h', 'now-1d')"),
		),
		mcp.WithString("status",
			mcp.Description("Filter by notification status (sent, error, pending, silenced, repeat-interval, inhibited, pending_playbook_run, pending_playbook_completion, evaluating-waitfor, attempting_fallback)"),
		),
		mcp.WithArray("select",
			mcp.WithStringItems(),
			mcp.Description("Array of field names to return (default: [\"id\",\"status\",\"count\",\"resource_health\",\"resource_status\",\"resource_health_description\",\"first_observed\",\"source_event\",\"created_at\",\"resource_id\",\"notification_id\"])"),
		),
	)
	s.AddTool(getNotificationsForResourceTool, getNotificationsForResourceHandler)
}
