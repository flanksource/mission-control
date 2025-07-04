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
	// TODO: Query not implemented (use peg search)
	//q, err := req.RequireString("query")
	//if err != nil {
	//return mcp.NewToolResultError(err.Error()), nil
	//}

	catalogID, err := req.RequireString("catalog_id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	cis, err := query.FindCatalogChanges(ctx, query.CatalogChangesSearchRequest{CatalogID: catalogID})
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

	searchCatalogTool := mcp.NewTool("catalog_search",
		mcp.WithDescription("Search across catalog"),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("Search query"),
		),
	)
	s.AddTool(searchCatalogTool, searchCatalogHandler)

	searchCatalogChangesTool := mcp.NewTool("catalog_changes_search",
		mcp.WithDescription("Search across catalog changes"),
		mcp.WithString("catalog_id",
			mcp.Description("Catalog ID"),
		),
		mcp.WithString("query",
			mcp.Description("Search query"),
		),
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
}
