package mcp

import (
	gocontext "context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
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
	toolListAllViews = "list_all_views"

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
		withRows:      args.Bool("withRows", false),
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

	return structToMCPResponse(contents...), nil
}

func viewListToolHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	currentViewToolsMu.RLock()
	keys := slices.Collect(maps.Keys(currentViewTools))
	currentViewToolsMu.RUnlock()

	jsonData, err := json.Marshal(keys)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), err
	}
	return mcp.NewToolResultText(string(jsonData)), nil
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

func syncViewsAsTools(ctx context.Context, s *server.MCPServer) error {
	views, err := gorm.G[models.View](ctx.DB()).Where("deleted_at IS NULL").Find(ctx)
	if err != nil {
		return fmt.Errorf("error fetching views: %w", err)
	}
	var newViewTools []string
	for _, view := range views {
		var spec v1.ViewSpec
		if err := json.Unmarshal(view.Spec, &spec); err != nil {
			return fmt.Errorf("error unmarshaling view[%s] spec: %w", view.ID, err)
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
			Default:     false,
			Description: "Include table rows for this view (paginated).",
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

		// Views can have template variables that we need to expose as parameters
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
			return fmt.Errorf("error marshaling root json schema: %w", err)
		}

		toolName := generateViewToolName(view)
		columnSummary := strings.TrimPrefix(columnDesc, "Select columns to include in the result. ")
		description := fmt.Sprintf(
			`Execute view %s [%s/%s]. %s %s.
Panels are excluded unless withPanels=true. .
To retrieve table rows use withRows=true; select/page/limit apply only when withRows is enabled.
Use the select array to request only the columns you truly need to minimize response tokens.

Without withRows/withPanels set, nothing is returned`,
			spec.Display.Title, view.Namespace, view.Name, spec.Description, columnSummary,
		)
		s.AddTool(mcp.NewToolWithRawSchema(toolName, description, rj), viewRunHandler)
		newViewTools = append(newViewTools, toolName)

		currentViewToolsMu.Lock()
		currentViewTools[toolName] = viewNamespaceName{Namespace: view.Namespace, Name: view.Name}
		currentViewToolsMu.Unlock()
	}

	// Delete old views and update currentViewTools list
	currentViewToolsMu.Lock()
	currentToolNames := slices.Collect(maps.Keys(currentViewTools))
	_, viewToolsToDelete := lo.Difference(newViewTools, currentToolNames)
	s.DeleteTools(viewToolsToDelete...)
	// Remove from currentViewTools
	maps.DeleteFunc(currentViewTools, func(k string, v viewNamespaceName) bool {
		return slices.Contains(viewToolsToDelete, k)
	})
	currentViewToolsMu.Unlock()

	return nil
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

	s.AddTool(mcp.NewTool(toolListAllViews, mcp.WithDescription("List all available view tools")), viewListToolHandler)
}
