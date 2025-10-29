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
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/views"
	"github.com/invopop/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/samber/lo"
	orderedmap "github.com/wk8/go-ordered-map/v2"
	"gorm.io/gorm"
)

func getViewHandler(goctx gocontext.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return nil, err
	}

	namespace, err := requireString(req, "namespace")
	if err != nil {
		return newToolResultError(err.Error()), nil
	}
	name, err := requireString(req, "name")
	if err != nil {
		return newToolResultError(err.Error()), nil
	}

	response, err := views.ReadOrPopulateViewTable(ctx, namespace, name)
	if err != nil {
		return newToolResultError(err.Error()), nil
	}
	return structToMCPResponse(response), nil
}

// ToolName -> View ID
var currentViewTools = make(map[string]viewNamespaceName)

type viewNamespaceName struct {
	Namespace string
	Name      string
}

func viewRunHandler(goctx gocontext.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return newToolResultError(err.Error()), nil
	}

	viewToolName := req.Params.Name
	v := currentViewTools[viewToolName]
	if v.Name == "" {
		return newToolResultError(fmt.Sprintf("tool[%s] is not associated with any view", viewToolName)), nil
	}

	response, err := views.ReadOrPopulateViewTable(ctx, v.Namespace, v.Name)
	if err != nil {
		return newToolResultError(err.Error()), nil
	}
	return structToMCPResponse(response), nil
}

func viewListToolHandler(goctx gocontext.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	jsonData, err := json.Marshal(slices.Collect(maps.Keys(currentViewTools)))
	if err != nil {
		return newToolResultError(err.Error()), err
	}
	return newToolResultText(string(jsonData)), nil
}

func viewResourceHandler(goctx gocontext.Context, req *mcp.ReadResourceRequest) ([]*mcp.ResourceContents, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return nil, err
	}

	namespace, name, err := extractNamespaceName(req.Params.URI)
	if err != nil {
		return nil, fmt.Errorf("invalid connection format: %s", req.Params.URI)
	}

	view, err := gorm.G[models.View](ctx.DB()).Where("namespace = ? AND name = ?", namespace, name).First(ctx)
	if err != nil {
		return nil, err
	}

	jsonData, err := json.Marshal(view)
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

func syncViewsAsTools(ctx context.Context, s *mcp.Server) error {
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
		s.AddTool(&mcp.Tool{
			Name:        toolName,
			Description: description),
			InputSchema: json.RawMessage(rj),
		}, viewRunHandler)
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

func registerViews(ctx context.Context, s *mcp.Server) {
	s.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: "view://{namespace}/{name}",
		Name:        "View",
		Description: "View resource data"),
		MIMEType:    lo.ToPtr("application/json"),
	}, viewResourceHandler)

	s.AddTool(&mcp.Tool{
		Name:        "list_all_views",
		Description: "List all available view tools"),
	}, viewListToolHandler)

	s.AddTool(&mcp.Tool{
		Name: "get_view",
		Description: `Retrieve dashboard view data containing business metrics and operational data.
		Views include tabular data (display as markdown tables) and chart panels (show panel data if visualization unavailable).
		Use list_views first to discover available views by their namespace and name.`),
		InputSchema: createInputSchema(map[string]any{
			"namespace": map[string]any{
				"type":        "string",
				"description": "Namespace of the view",
			},
			"name": map[string]any{
				"type":        "string",
				"description": "Name of the view",
			},
		}, nil),
	}, getViewHandler)
}
