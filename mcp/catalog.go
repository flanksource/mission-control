package mcp

import (
	gocontext "context"
	"encoding/json"
	"fmt"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func searchCatalogHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	q, err := req.RequireString("query")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	cis, err := query.FindConfigsByResourceSelector(ctx, 10, types.ResourceSelector{Search: q})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	jsonData, err := json.Marshal(cis)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	return mcp.NewToolResultText(string(jsonData)), nil
}

func searchConfigChangesHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	q, err := req.RequireString("query")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	limit := req.GetInt("limit", 20)

	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	cis, err := query.FindConfigChangesByResourceSelector(ctx, limit, types.ResourceSelector{Search: q})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	jsonData, err := json.Marshal(cis)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	return mcp.NewToolResultText(string(jsonData)), nil
}

func relatedCatalogHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	rawID, err := req.RequireString("id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	id, err := uuid.Parse(rawID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	cis, err := query.GetRelatedConfigs(ctx, query.RelationQuery{
		ID: id,
	})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	jsonData, err := json.Marshal(cis)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	return mcp.NewToolResultText(string(jsonData)), nil
}

func configTypeResourceHandler(goctx gocontext.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return nil, err
	}

	var types []string
	err = ctx.DB().Model(&models.ConfigItem{}).Select("DISTINCT(type)").Find(&types).Error
	if err != nil {
		return nil, err
	}

	jsonData, err := json.Marshal(types)
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

func ConfigItemResourceHandler(goctx gocontext.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	id := extractID(req.Params.URI)

	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return nil, err
	}
	ci, err := query.GetCachedConfig(ctx, id)
	if err != nil {
		return nil, err
	}
	if ci == nil {
		return nil, err
	}
	jsonData, err := json.Marshal(ci)
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

func searchCatalogPromptHandler(ctx gocontext.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	query := req.Params.Arguments["query"]

	return &mcp.GetPromptResult{
		Description: fmt.Sprintf("Catalog search for %s query", query),
		Messages: []mcp.PromptMessage{
			{
				Role: "user",
				Content: mcp.NewTextContent(fmt.Sprintf(
					`
					`)),
			},
		},
	}, nil
}

func getDutyCtx(ctx gocontext.Context) (context.Context, error) {
	if v := ctx.Value(dutyContextKey); v != nil {
		dutyCtx, ok := v.(context.Context)
		if ok {
			return dutyCtx, nil
		}
	}
	return context.Context{}, fmt.Errorf("no duty ctx")
}

func registerCatalog(s *server.MCPServer) {
	s.AddResourceTemplate(
		mcp.NewResourceTemplate("config_item://{id}", "Config Item",
			mcp.WithTemplateDescription("Config Item Data"), mcp.WithTemplateMIMEType(echo.MIMEApplicationJSON)),
		ConfigItemResourceHandler)

	s.AddResource(mcp.NewResource("config_item://config_types", "Config Types",
		mcp.WithResourceDescription("List all config types"), mcp.WithMIMEType(echo.MIMEApplicationJSON)),
		configTypeResourceHandler)

	var queryDescription = `
	Using the below args, query the catalog search tool:
	type could be in the format of GCP::xxx, Kubernetes::xxx, Azure::xxx, AWS::xxx
	Use the resource config_item://config_types to get the list

	Fields we support in query: id, agent, name, namespace, labelSelector, tagSelector, status, health, limit (max items to return)
	Example queries:
	- Query all kubernetes namespaces that start with kube is -> type=Kubernetes::Namespace kube*
	- Query all unhealthy ec2 instances -> type=AWS::EC2::Instance health=unhealthy

	Whatever comes after the first :: is the config_class

	For health we use healthy, unhealthy, warning and unknown

	And we support * searches for prefix (pattern*), suffix (*pattern) and glob (*pattern*)

	If you are asked things like name contains or type contains, use "*" around it, for example, name contains
	postgres should mean query -> name=*postgres*

	Status can be a lot of different things

	When you are asked to query, its in peg format "type=Kuberntes::Deployment name=nginx*" which fetches all nginx deployments
	the query, "type=Kuberntes::* health=unhealthy" will get all unhealthy kubernetes resources

	`
	searchCatalogTool := mcp.NewTool("catalog_search",
		mcp.WithDescription("Search across catalog"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("Search query"+queryDescription),
		),
	)
	s.AddTool(searchCatalogTool, searchCatalogHandler)

	var configChangeQueryDescription = `
	Using the below args, query the catalog search tool:
	type could be in the format of GCP::xxx, Kubernetes::xxx, Azure::xxx, AWS::xxx
	Use the resource config_item://change_types to get the list

	Fields we support in query: "id", "config_id", "name", "type", "created_at", "severity", "change_type", "summary", "count", "first_observed", "agent_id"
	Example queries:
	- Query all changes for config_id in last 7 days is -> config_id=7f8d56ee-55cb-468e-9f93-7d46f9a94f44 created_at=now-7d

	For severity we use info,low,medium,high and critical

	And we support * searches for prefix (pattern*), suffix (*pattern) and glob (*pattern*)

	If you are asked things like name contains or type contains, use "*" around it, for example, name contains
	postgres should mean query -> name=*postgres*

	When you are asked to query, its in peg format "type=Kuberntes::Deployment name=nginx*" which fetches all nginx deployments
	the query, "type=Kuberntes::* health=unhealthy" will get all unhealthy kubernetes resources
	`

	searchCatalogChangesTool := mcp.NewTool("catalog_changes_search",
		mcp.WithDescription("Search across catalog changes"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("Search query"+configChangeQueryDescription),
		),
		mcp.WithNumber("limit", mcp.Description("Number of results to return")),
	)
	s.AddTool(searchCatalogChangesTool, searchConfigChangesHandler)

	relatedCatalogTool := mcp.NewTool("related_configs",
		mcp.WithDescription("Get related configs"),
		mcp.WithString("id",
			mcp.Required(),
			mcp.Description("Config ID"),
		),
	)
	s.AddTool(relatedCatalogTool, relatedCatalogHandler)

	s.AddPrompt(mcp.NewPrompt("Search catalog prompt"), searchCatalogPromptHandler)
}
