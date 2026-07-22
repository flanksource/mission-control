package mcp

import (
	gocontext "context"
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"sync"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	pkgView "github.com/flanksource/duty/view"
	"github.com/invopop/jsonschema"
	"github.com/labstack/echo/v4"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/samber/lo"
	orderedmap "github.com/wk8/go-ordered-map/v2"
	"gorm.io/gorm"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/views"
)

const (
	viewDefaultLimit = 50
	viewDefaultPage  = 1
	viewMaxLimit     = 500
)

// ToolName -> View ID
var currentViewTools = make(map[string]viewNamespaceName)
var currentViewToolsMu sync.RWMutex

type viewNamespaceName struct {
	Namespace string
	Name      string
}

type viewRequest struct {
	withPanels    bool
	withRows      bool
	selectColumns []string
	page          int
	limit         int
}

func parseViewOptions(args ArgParser) viewRequest {
	return viewRequest{
		withPanels:    args.Bool("withPanels", false),
		withRows:      args.Bool("withRows", true),
		selectColumns: args.Strings("select"),
		page:          args.Int("page", viewDefaultPage),
		limit:         args.Int("limit", 0),
	}
}

func filterRequestedColumns(requested []string, available []pkgView.ColumnDef) []string {
	if len(requested) == 0 {
		return nil
	}

	validColumns := make(map[string]struct{}, len(available))
	for _, column := range available {
		validColumns[column.Name] = struct{}{}
	}

	filtered := make([]string, 0, len(requested))
	for _, column := range requested {
		if column = strings.TrimSpace(column); column == "" {
			continue
		}

		if _, ok := validColumns[column]; ok {
			filtered = append(filtered, column)
		}
	}

	return filtered
}

func extractTemplateVars(args map[string]any) map[string]string {
	vars := make(map[string]string)
	for k, v := range args {
		if isReservedViewArg(k) {
			continue
		}
		vars[k] = fmt.Sprint(v)
	}
	return vars
}

func isReservedViewArg(key string) bool {
	switch strings.ToLower(key) {
	case "withrows", "withpanels", "withmetadata", "select", "page", "limit", "namespace", "name":
		return true
	default:
		return false
	}
}

func buildColumnsDescription(columns []pkgView.ColumnDef) ([]any, string) {
	var names []any
	var descParts []string
	for _, col := range columns {
		names = append(names, col.Name)
		descParts = append(descParts, fmt.Sprintf("%s (type=%s)", col.Name, col.Type))
	}

	desc := "Select columns to include in the result. Available columns: " + strings.Join(descParts, ", ")
	return names, desc
}

func buildViewRequestOptions(args map[string]any) []views.ViewOption {
	templateVars := extractTemplateVars(args)
	viewOpts := make([]views.ViewOption, 0, len(templateVars))
	for key, value := range templateVars {
		viewOpts = append(viewOpts, views.WithVariable(key, value))
	}
	return viewOpts
}

func fetchViewData(ctx context.Context, namespace, name string, args ArgParser) ([]api.PanelResult, []map[string]any, error) {
	opts := parseViewOptions(args)
	var (
		rows     []map[string]any
		response *api.ViewResult
	)
	err := auth.WithRLS(ctx, func(rlsCtx context.Context) error {
		viewOpts := buildViewRequestOptions(args.Raw())
		var err error
		response, err = views.ReadOrPopulateViewTable(rlsCtx, namespace, name, viewOpts...)
		if err != nil {
			return err
		}

		normalizePagination(&opts, response)

		if opts.withRows {
			requestedColumns := filterRequestedColumns(opts.selectColumns, response.Columns)
			rows, err = readViewRows(rlsCtx, namespace, name, response.RequestFingerprint, requestedColumns, opts.page, opts.limit)
			if err != nil {
				return err
			}
		}

		if !opts.withPanels {
			response.Panels = nil
		}

		return nil
	})

	if err != nil || response == nil {
		return nil, rows, err
	}

	return response.Panels, rows, nil
}

func viewRunHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	viewToolName := req.Params.Name
	currentViewToolsMu.RLock()
	v := currentViewTools[viewToolName]
	currentViewToolsMu.RUnlock()
	if v.Name == "" {
		return mcp.NewToolResultError(fmt.Sprintf("tool[%s] is not associated with any view", viewToolName)), nil
	}

	owner, err := resolveOwner(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	var view models.View
	err = auth.WithRLS(ctx, func(rlsCtx context.Context) error {
		view, err = gorm.G[models.View](rlsCtx.DB()).Where("namespace = ? AND name = ? AND deleted_at IS NULL", v.Namespace, v.Name).First(rlsCtx)
		return err
	})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	attr := &models.ABACAttribute{View: view}
	if !rbac.HasPermission(ctx, owner, attr, policy.ActionMCPRun) {
		return mcp.NewToolResultError(fmt.Sprintf("forbidden: mcp:run not permitted on view %s/%s", view.Namespace, view.Name)), nil
	}

	args := NewArgParser(req.Params.Arguments)
	panels, rows, err := fetchViewData(ctx, v.Namespace, v.Name, args)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	// We send rows, then (panel meta, panel rows) as args to clicky.
	// It produces a markdown table for each item.
	contents := make([]any, 0, 1+len(panels)*2)
	if args.Bool("withRows", false) {
		contents = append(contents, rows)
	}
	if args.Bool("withPanels", false) {
		for _, panel := range panels {
			rows := panel.Rows
			panel.Rows = nil
			contents = append(contents, []api.PanelResult{panel}, rows)
		}
	}
	if len(contents) == 0 {
		return mcp.NewToolResultText(""), nil
	}

	return structToMCPResponse(req, contents...), nil
}

func viewResourceHandler(goctx gocontext.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return nil, err
	}

	namespace, name, err := extractNamespaceName(req.Params.URI)
	if err != nil {
		return nil, fmt.Errorf("invalid connection format: %s", req.Params.URI)
	}

	var view models.View
	err = auth.WithRLS(ctx, func(rlsCtx context.Context) error {
		view, err = gorm.G[models.View](rlsCtx.DB()).Where("namespace = ? AND name = ?", namespace, name).First(rlsCtx)
		return err
	})

	if err != nil {
		return nil, err
	}

	jsonData, err := json.Marshal(view)
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

func addViewsAsTool(goctx gocontext.Context, srv *server.MCPServer, session server.ClientSession) error {
	sessionID := session.SessionID()

	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return fmt.Errorf("failed to get duty context for session %s: %w", sessionID, err)
	}

	var allViews []models.View
	err = auth.WithRLS(ctx, func(txCtx context.Context) error {
		var err error
		allViews, err = gorm.G[models.View](txCtx.DB()).Where("deleted_at IS NULL").Find(txCtx)
		return err
	})
	if err != nil {
		return ctx.Oops().Wrapf(err, "failed to get view tools for session %s", sessionID)
	}

	owner, err := resolveOwner(ctx)
	if err != nil {
		return ctx.Oops().Wrap(err)
	}

	permittedViews := lo.Filter(allViews, func(view models.View, _ int) bool {
		attr := &models.ABACAttribute{View: view}
		return rbac.HasPermission(ctx, owner, attr, policy.ActionMCPRun)
	})

	tools, toolsMap, err := getViewsAsTools(ctx, permittedViews)
	if err != nil {
		return ctx.Oops().Wrapf(err, "failed to convert views to tools for session %s", sessionID)
	}

	currentViewToolsMu.Lock()
	maps.Copy(currentViewTools, toolsMap)
	currentViewToolsMu.Unlock()

	sessionTools := lo.Map(tools, func(tool mcp.Tool, _ int) server.ServerTool {
		return server.ServerTool{
			Tool:    tool,
			Handler: viewRunHandler,
		}
	})
	if err := srv.AddSessionTools(sessionID, sessionTools...); err != nil {
		return ctx.Oops().Wrapf(err, "failed to add tool %d to session %s", len(sessionTools), sessionID)
	}

	ctx.Logger.Infof("Successfully registered %d view tools for session %q", len(tools), sessionID)
	return nil
}

func getViewsAsTools(ctx context.Context, views []models.View) ([]mcp.Tool, map[string]viewNamespaceName, error) {
	newViewTools := make([]mcp.Tool, 0, len(views))
	newViewToolsMap := make(map[string]viewNamespaceName, len(views))

	for _, view := range views {
		var spec v1.ViewSpec
		if err := json.Unmarshal(view.Spec, &spec); err != nil {
			ctx.Logger.Warnf("skipping view[%s]: error unmarshaling spec: %v", view.ID, err)
			continue
		}

		root := &jsonschema.Schema{
			Type:       "object",
			Required:   []string{},
			Properties: orderedmap.New[string, *jsonschema.Schema](),
		}

		columnNames, columnDesc := buildColumnsDescription(spec.Columns)
		root.Properties.Set("withPanels", &jsonschema.Schema{
			Type:        "boolean",
			Default:     false,
			Description: "Include panel data (from view_panels table). Disabled by default.",
		})
		root.Properties.Set("withRows", &jsonschema.Schema{
			Type:        "boolean",
			Default:     true,
			Description: "Include table rows for this view (paginated). Enabled by default.",
		})
		root.Properties.Set("select", &jsonschema.Schema{
			Type:        "array",
			Description: columnDesc,
			Items: &jsonschema.Schema{
				Type: "string",
				Enum: columnNames,
			},
		})
		root.Properties.Set("page", &jsonschema.Schema{
			Type:        "integer",
			Default:     viewDefaultPage,
			Description: "Page number (1-based).",
		})
		root.Properties.Set("limit", &jsonschema.Schema{
			Type:        "integer",
			Default:     viewDefaultLimit,
			Description: fmt.Sprintf("Rows per page (max %d).", viewMaxLimit),
		})

		if len(spec.Templating) > 0 {
			for _, template := range spec.Templating {
				s := &jsonschema.Schema{Type: "string", Description: template.Label}
				if template.Default != "" {
					s.Default = template.Default
				}
				if len(template.Values) > 0 {
					s.Enum = make([]any, len(template.Values))
					for i, opt := range template.Values {
						s.Enum[i] = opt
					}
				}
				root.Properties.Set(template.Key, s)
			}
		}

		rj, err := root.MarshalJSON()
		if err != nil {
			ctx.Logger.Warnf("skipping view[%s]: error marshaling json schema: %v", view.ID, err)
			continue
		}

		toolName := generateViewToolName(view)
		columnSummary := strings.TrimPrefix(columnDesc, "Select columns to include in the result. ")
		baseDesc := lo.CoalesceOrEmpty(spec.MCP.Description, spec.Description)
		if len(spec.MCP.Tags) > 0 {
			baseDesc = fmt.Sprintf("%s [tags: %s]", baseDesc, strings.Join(spec.MCP.Tags, ", "))
		}
		description := fmt.Sprintf(
			`Execute view %s [%s/%s]. %s %s.
Table rows are returned by default (withRows=true); use select/page/limit to control output.
Panels are excluded unless withPanels=true.
Use the select array to request only the columns you truly need to minimize response tokens.`,
			spec.Display.Title, view.Namespace, view.Name, baseDesc, columnSummary,
		)
		viewTool := mcp.NewToolWithRawSchema(toolName, description, rj)
		viewTool.Annotations.Title = lo.CoalesceOrEmpty(spec.MCP.Title, spec.Display.Title)
		viewTool.Annotations.ReadOnlyHint = lo.CoalesceOrEmpty(spec.MCP.ReadOnlyHint, lo.ToPtr(true))
		viewTool.Annotations.DestructiveHint = spec.MCP.DestructiveHint
		viewTool.Annotations.IdempotentHint = spec.MCP.IdempotentHint
		viewTool.Annotations.OpenWorldHint = spec.MCP.OpenWorldHint
		newViewTools = append(newViewTools, viewTool)
		newViewToolsMap[toolName] = viewNamespaceName{Namespace: view.Namespace, Name: view.Name}
	}

	return newViewTools, newViewToolsMap, nil
}

func generateViewToolName(view models.View) string {
	name := strings.ToLower(strings.ReplaceAll(view.Name, " ", "-"))
	toolName := strings.ToLower(fmt.Sprintf("view_%s_%s", name, view.Namespace))
	return fixMCPToolNameIfRequired(toolName)
}

func registerViews(s *server.MCPServer) {
	s.AddResourceTemplate(
		mcp.NewResourceTemplate("view://{namespace}/{name}", "View",
			mcp.WithTemplateDescription("View resource data"), mcp.WithTemplateMIMEType(echo.MIMEApplicationJSON)),
		viewResourceHandler,
	)

}
