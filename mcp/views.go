package mcp

import (
	gocontext "context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/views"
	"github.com/invopop/jsonschema"
	"github.com/labstack/echo/v4"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/samber/lo"
	orderedmap "github.com/wk8/go-ordered-map/v2"
	"gorm.io/gorm"
)

func getViewHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return nil, err
	}

	namespace, err := req.RequireString("namespace")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	var response *api.ViewResult
	err = auth.WithRLS(ctx, func(rlsCtx context.Context) error {
		response, err = views.ReadOrPopulateViewTable(rlsCtx, namespace, name)
		return err
	})

	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return structToMCPResponse(response), nil
}

// ToolName -> View ID
var currentViewTools = make(map[string]viewNamespaceName)

type viewNamespaceName struct {
	Namespace string
	Name      string
}

func viewRunHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	viewToolName := req.Params.Name
	v := currentViewTools[viewToolName]
	if v.Name == "" {
		return mcp.NewToolResultError(fmt.Sprintf("tool[%s] is not associated with any view", viewToolName)), nil
	}

	var response *api.ViewResult
	err = auth.WithRLS(ctx, func(rlsCtx context.Context) error {
		response, err = views.ReadOrPopulateViewTable(rlsCtx, v.Namespace, v.Name)
		return err
	})

	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return structToMCPResponse(response), nil
}

func viewListToolHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	jsonData, err := json.Marshal(slices.Collect(maps.Keys(currentViewTools)))
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
		description := fmt.Sprintf("Execute view %s [%s/%s]. %s", spec.Display.Title, view.Namespace, view.Name, spec.Description)
		s.AddTool(mcp.NewToolWithRawSchema(toolName, description, rj), viewRunHandler)
		newViewTools = append(newViewTools, toolName)
		currentViewTools[toolName] = viewNamespaceName{Namespace: view.Namespace, Name: view.Name}
	}

	// Delete old views and update currentViewTools list
	currentToolNames := slices.Collect(maps.Keys(currentViewTools))
	_, viewToolsToDelete := lo.Difference(newViewTools, currentToolNames)
	s.DeleteTools(viewToolsToDelete...)
	// Remove from currentViewTools
	maps.DeleteFunc(currentViewTools, func(k string, v viewNamespaceName) bool {
		return slices.Contains(viewToolsToDelete, k)
	})

	return nil
}

func generateViewToolName(view models.View) string {
	name := strings.ToLower(strings.ReplaceAll(view.Name, " ", "-"))
	toolName := strings.ToLower(fmt.Sprintf("view_%s_%s", name, view.Namespace))
	return fixMCPToolNameIfRequired(toolName)
}

func registerViews(ctx context.Context, s *server.MCPServer) {
	s.AddResourceTemplate(
		mcp.NewResourceTemplate("view://{namespace}/{name}", "View",
			mcp.WithTemplateDescription("View resource data"), mcp.WithTemplateMIMEType(echo.MIMEApplicationJSON)),
		viewResourceHandler,
	)

	s.AddTool(mcp.NewTool("list_all_views",
		mcp.WithDescription("List all available view tools")), viewListToolHandler)

	s.AddTool(mcp.NewTool("get_view",
		mcp.WithDescription(`Retrieve dashboard view data containing business metrics and operational data.
		Views include tabular data (display as markdown tables) and chart panels (show panel data if visualization unavailable).
		Use list_views first to discover available views by their namespace and name.`),
		mcp.WithString("namespace", mcp.Description("Namespace of the view")),
		mcp.WithString("name", mcp.Description("Name of the view")),
	), getViewHandler)
}
