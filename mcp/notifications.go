package mcp

import (
	gocontext "context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/samber/lo"
	"gorm.io/gorm"

	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/notification"
	icrbac "github.com/flanksource/incident-commander/rbac"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/timberio/go-datemath"
)

// recipient names who a send went to: its connection, person or team, or the playbook it ran.
func recipient(connection string, personName, teamName *string, playbookRunID *uuid.UUID) string {
	switch {
	case connection != "":
		return "connection:" + connection
	case personName != nil:
		return "person:" + *personName
	case teamName != nil:
		return "team:" + *teamName
	case playbookRunID != nil:
		return "playbook_run:" + playbookRunID.String()
	}
	return ""
}

// connectionName is the connection's namespace/name, or its ID when it's gone.
func connectionName(connections map[uuid.UUID]string, id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return lo.CoalesceOrEmpty(connections[*id], id.String())
}

// connectionNames returns the namespace/name of the connections. It reads outside RLS, as connections are an
// internal table, and returns only their names: a send's error already names its connection.
func connectionNames(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var conns []models.Connection
	if err := ctx.DB().Select("id", "namespace", "name").Where("id IN ?", lo.Uniq(ids)).Find(&conns).Error; err != nil {
		return nil, err
	}
	return lo.SliceToMap(conns, func(c models.Connection) (uuid.UUID, string) { return c.ID, c.Namespace + "/" + c.Name }), nil
}

// notificationSendRow is a send in list_notification_sends.
type notificationSendRow struct {
	ID                    uuid.UUID  `json:"id"`
	CreatedAt             time.Time  `json:"created_at"`
	FirstObserved         time.Time  `json:"first_observed"`
	Count                 int        `json:"count"`
	Status                string     `json:"status"`
	SourceEvent           string     `json:"source_event"`
	ResourceID            uuid.UUID  `json:"resource_id"`
	ResourceKind          *string    `json:"resource_kind,omitempty"`
	ResourceType          *string    `json:"resource_type,omitempty"`
	ResourceName          *string    `json:"resource_name,omitempty"`
	NotificationID        uuid.UUID  `json:"notification_id"`
	NotificationName      *string    `json:"notification_name,omitempty"`
	NotificationNamespace *string    `json:"notification_namespace,omitempty"`
	ParentID              *uuid.UUID `json:"parent_id,omitempty"`
	ConnectionID          *uuid.UUID `json:"connection_id,omitempty"`
	PlaybookRunID         *uuid.UUID `json:"playbook_run_id,omitempty"`
	PersonName            *string    `json:"person_name,omitempty"`
	TeamName              *string    `json:"team_name,omitempty"`
	Connection            string     `json:"connection,omitempty" gorm:"-"`
	Recipient             string     `json:"recipient,omitempty" gorm:"-"`
	Error                 *string    `json:"error,omitempty"`
}

// notificationRule is the notification a send was made for, without its templates and recipient URLs.
type notificationRule struct {
	ID             uuid.UUID      `json:"id"`
	Name           string         `json:"name"`
	Namespace      string         `json:"namespace,omitempty"`
	Events         pq.StringArray `json:"events" gorm:"type:[]text"`
	Title          string         `json:"title,omitempty"`
	Filter         string         `json:"filter,omitempty"`
	Source         string         `json:"source,omitempty"`
	RepeatInterval string         `json:"repeat_interval,omitempty"`
	Error          *string        `json:"error,omitempty"`
	ErrorAt        *time.Time     `json:"error_at,omitempty"`
	DeletedAt      *time.Time     `json:"deleted_at,omitempty"`
}

type notificationDetailResponse struct {
	models.NotificationSendHistory `json:",inline"`
	ResourceKind                   *string           `json:"resource_kind,omitempty"`
	ResourceType                   *string           `json:"resource_type,omitempty"`
	Resource                       types.JSON        `json:"resource,omitempty"`
	PlaybookRun                    types.JSON        `json:"playbook_run,omitempty"`
	PersonName                     *string           `json:"person_name,omitempty"`
	TeamName                       *string           `json:"team_name,omitempty"`
	Connection                     string            `json:"connection,omitempty" gorm:"-"`
	Recipient                      string            `json:"recipient,omitempty" gorm:"-"`
	Notification                   *notificationRule `json:"notification,omitempty" gorm:"-"`
	BodyMarkdown                   string            `json:"body_markdown,omitempty" gorm:"-"`
}

const (
	toolGetNotificationDetail       = "get_notification_detail"
	toolGetNotificationsForResource = "get_notifications_for_resource"
	toolListNotificationSends       = "list_notification_sends"
)

// notificationSendJoins joins a send in notification_send_history_summary (h) to its notification (n), person and team.
const notificationSendJoins = `LEFT JOIN notifications n ON n.id = h.notification_id
LEFT JOIN people p ON p.id = h.person_id
LEFT JOIN teams t ON t.id = h.team_id`

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
	if err := requireNotificationRead(ctx); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	sendID, err := req.RequireString("send_id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if _, err := uuid.Parse(sendID); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid send_id %q: must be a UUID", sendID)), nil
	}

	var resp notificationDetailResponse
	err = auth.WithRLS(ctx, func(rlsCtx context.Context) error {
		err := rlsCtx.DB().
			Table("notification_send_history_summary AS h").
			Select("h.*, p.name AS person_name, t.name AS team_name").
			Joins(notificationSendJoins).
			Where("h.id = ?", sendID).
			Take(&resp).Error
		if err != nil {
			return err
		}

		var rule notificationRule
		if err := rlsCtx.DB().Table("notifications").Where("id = ?", resp.NotificationID).Take(&rule).Error; err == nil {
			resp.Notification = &rule
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return mcp.NewToolResultError(fmt.Sprintf("notification send %s not found", sendID)), nil
	} else if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to get notification: %v", err)), nil
	}

	connections, err := connectionNames(ctx, lo.FromSlicePtr([]*uuid.UUID{resp.ConnectionID}))
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to get the send's connection: %v", err)), nil
	}
	resp.Connection = connectionName(connections, resp.ConnectionID)
	resp.Recipient = recipient(resp.Connection, resp.PersonName, resp.TeamName, resp.PlaybookRunID)
	resp.BodyMarkdown = notification.RenderBodyMarkdown(resp.NotificationSendHistory)

	return structToMCPResponse(req, resp), nil
}

func listNotificationSendsHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := requireNotificationRead(ctx); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	limit := req.GetInt("limit", 20)
	if limit <= 0 || limit > 200 {
		return mcp.NewToolResultError("limit must be between 1 and 200"), nil
	}

	var filters []func(*gorm.DB) *gorm.DB
	where := func(query string, args ...any) {
		filters = append(filters, func(db *gorm.DB) *gorm.DB { return db.Where(query, args...) })
	}

	if status := req.GetString("status", ""); status != "" {
		where("h.status IN ?", lo.Map(strings.Split(status, ","), func(s string, _ int) string { return strings.TrimSpace(s) }))
	}
	if n := req.GetString("notification", ""); n != "" {
		if id, err := uuid.Parse(n); err == nil {
			where("h.notification_id = ?", id)
		} else if namespace, name, ok := strings.Cut(n, "/"); ok {
			where("n.namespace = ? AND n.name = ?", namespace, name)
		} else {
			where("n.name = ?", n)
		}
	}
	if resourceID := req.GetString("resource_id", ""); resourceID != "" {
		if _, err := uuid.Parse(resourceID); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid resource_id %q: must be a UUID", resourceID)), nil
		}
		where("h.resource_id = ?", resourceID)
	}
	if connection := req.GetString("connection", ""); connection != "" {
		if id, err := uuid.Parse(connection); err == nil {
			where("h.connection_id = ?", id)
		} else {
			q := ctx.DB().Model(&models.Connection{}).Select("id")
			if namespace, name, ok := strings.Cut(connection, "/"); ok {
				q = q.Where("namespace = ? AND name = ?", namespace, name)
			} else {
				q = q.Where("name = ?", connection)
			}
			var ids []uuid.UUID
			if err := q.Pluck("id", &ids).Error; err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("failed to find connection %s: %v", connection, err)), nil
			}
			where("h.connection_id IN ?", append(ids, uuid.Nil))
		}
	}
	for param, op := range map[string]string{"since": ">=", "before": "<="} {
		if val := req.GetString(param, ""); val != "" {
			t, err := parseDateMath(val)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("invalid %s parameter: %v", param, err)), nil
			}
			where("h.created_at "+op+" ?", t)
		}
	}

	var rows []notificationSendRow
	err = auth.WithRLS(ctx, func(rlsCtx context.Context) error {
		return rlsCtx.DB().
			Table("notification_send_history_summary AS h").
			Select(`h.id, h.created_at, h.first_observed, h.count, h.status, h.source_event, h.resource_id,
h.resource_kind, h.resource_type, h.resource->>'name' AS resource_name,
h.notification_id, n.name AS notification_name, n.namespace AS notification_namespace,
h.parent_id, h.connection_id, h.playbook_run_id, h.error, p.name AS person_name, t.name AS team_name`).
			Joins(notificationSendJoins).
			Scopes(filters...).
			Order("h.created_at DESC").
			Limit(limit).
			Find(&rows).Error
	})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to list notification sends: %v", err)), nil
	}

	connections, err := connectionNames(ctx, lo.FilterMap(rows, func(r notificationSendRow, _ int) (uuid.UUID, bool) {
		return lo.FromPtr(r.ConnectionID), r.ConnectionID != nil
	}))
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to get the sends' connections: %v", err)), nil
	}
	for i := range rows {
		r := &rows[i]
		r.Connection = connectionName(connections, r.ConnectionID)
		r.Recipient = recipient(r.Connection, r.PersonName, r.TeamName, r.PlaybookRunID)
	}

	return structToMCPResponse(req, rows), nil
}

func getNotificationsForResourceHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := requireNotificationRead(ctx); err != nil {
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

	return structToMCPResponse(req, notifications), nil
}

// requireNotificationRead checks read on notifications, whose send history the notification tools return.
// It's checked as the request's own subject, like the HTTP routes, so an access token's Roles apply.
func requireNotificationRead(ctx context.Context) error {
	if !icrbac.CheckContext(ctx, policy.ObjectNotification, policy.ActionRead) {
		return fmt.Errorf("forbidden: %s does not have %s permission on %s", ctx.Subject(), policy.ActionRead, policy.ObjectNotification)
	}
	return nil
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
	getNotificationDetailTool := mcp.NewTool(toolGetNotificationDetail,
		mcp.WithDescription("Get one notification send by ID: its status and full error, body, body_payload and body_markdown (rendered body), the original event payload, the recipient (person, team, connection or playbook run), the resource it was sent for, and the notification rule that sent it. Use list_notification_sends to find send IDs."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithString("send_id",
			mcp.Required(),
			mcp.Description("UUID of the notification send history record"),
		),
	)
	s.AddTool(getNotificationDetailTool, getNotificationDetailHandler)

	getNotificationsForResourceTool := mcp.NewTool(toolGetNotificationsForResource,
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
			mcp.Description("Filter by notification status (sending, sent, error, pending, silenced, repeat-interval, inhibited, pending_playbook_run, pending_playbook_completion, evaluating-waitfor, attempting_fallback)"),
		),
		mcp.WithArray("select",
			mcp.WithStringItems(),
			mcp.Description("Array of field names to return (default: [\"id\",\"status\",\"count\",\"resource_health\",\"resource_status\",\"resource_health_description\",\"first_observed\",\"source_event\",\"created_at\",\"resource_id\",\"notification_id\"])"),
		),
	)
	s.AddTool(getNotificationsForResourceTool, getNotificationsForResourceHandler)

	listNotificationSendsTool := mcp.NewTool(toolListNotificationSends,
		mcp.WithDescription("List recent notification sends across all notifications, newest first, to find failing or silenced notifications. Each row has the send's id, time, status, event, resource, notification name and namespace, recipient and error. Use get_notification_detail with a row's id for its body and full details."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithString("status",
			mcp.Description("Comma-separated statuses to include, e.g. 'error' or 'error,attempting_fallback' (sending, sent, error, pending, skipped, silenced, repeat-interval, inhibited, pending_playbook_run, pending_playbook_completion, evaluating-waitfor, attempting_fallback)"),
		),
		mcp.WithString("notification",
			mcp.Description("Notification ID, 'namespace/name', or name"),
		),
		mcp.WithString("resource_id",
			mcp.Description("UUID of the resource (config item, component, check or canary) the notification was sent for"),
		),
		mcp.WithString("connection",
			mcp.Description("Recipient connection ID, 'namespace/name', or name"),
		),
		mcp.WithString("since",
			mcp.Description("Start time filter using datemath expressions (e.g., 'now-24h', 'now-7d', 'now-30m')"),
		),
		mcp.WithString("before",
			mcp.Description("End time filter using datemath expressions (e.g., 'now-1h', 'now-1d')"),
		),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of sends to return, 1 to 200 (default: 20)"),
		),
	)
	s.AddTool(listNotificationSendsTool, listNotificationSendsHandler)
}
