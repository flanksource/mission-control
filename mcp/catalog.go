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

	limit := req.GetInt("limit", 30)

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

func configTypeResourceHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return nil, err
	}

	var types []string
	err = ctx.DB().Model(&models.ConfigItem{}).Select("DISTINCT(type)").Find(&types).Error
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	jsonData, err := json.Marshal(types)
	if err != nil {
		return nil, err
	}

	return mcp.NewToolResultText(string(jsonData)), nil
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
		return nil, fmt.Errorf("config item[%s] not found", id)
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

	s.AddTool(mcp.NewTool("list_catalog_types",
		mcp.WithDescription("List all config types")), configTypeResourceHandler)

	var queryDescription = `
	We can search our entire catalog via query
	Use the tool: list_catalog_types to get all the types first to make inference is better (cache them for 15m)

	FORMAL PEG GRAMMAR:
	Query = AndQuery _ OrQuery*
	OrQuery = _ '|' _ AndQuery
	AndQuery = _ FieldQuery _ FieldQuery*
	FieldQuery = _ '(' _ Query _ ')' _
	/ _ Field _
	/ _ '-' Word _
	/ _ (Word / Identifier) _
	Field = Source _ Operator _ Value
	Source = Identifier ('.' Identifier)*
	Operator = "<=" / ">=" / "=" / ":" / "!=" / "<" / ">"
	Value = DateTime / ISODate / Time / Measure
	/ Float / Integer / Identifier / String
	String = '"' [^"]* '"'
	ISODate = [0-9]{4} '-' [0-9]{2} '-' [0-9]{2}
	Time = [0-2][0-9] ':' [0-5][0-9] ':' [0-5][0-9]
	DateTime = "now" (("+" / "-") Integer DurationUnit)?
	/ ISODate ? Time?
	DurationUnit = "s" / "m" / "h" / "d" / "w" / "mo" / "y"
	Word = String / '-'? [@a-zA-Z0-9-]+
	Integer = [+-]?[0-9]+ ![a-zA-Z0-9_-]
	Float = [+-]? [0-9] '.' [0-9]+
	Measure = (Integer / Float) Identifier
	Identifier = [@a-zA-Z0-9_\,-:\[\]]+
	_ = [ \t]
	EOF = !.

	Query Shape
	A query is one or more space-separated pairs: field=value.
	Order does not matter.
	Fields:  type | id | agent | name | namespace | labelSelector | tagSelector | status | health | limit | created_at | updated_at | deleted_at | label.* | tag.*
	• label.* and tag.* accept any key after the dot, following Kubernetes label grammar.
	• labelSelector and tagSelector accept full Kubernetes selector expressions (e.g. env in (prod,stage), !deprecated).
	type: <Provider>::<ConfigClass> or <Provider>::* (Providers: AWS, Azure, GCP, Kubernetes)
	health: healthy | unhealthy | warning | unknown
	status free text (wildcards allowed)
	limit: positive integer
	date fields compare with <, >, <=, >= against:
	– absolute ISO date YYYY-MM-DD
	– date-math now±N{ s | m | h | d | w | mo | y } (e.g. now-24h, now-7d)
	WILDCARDS
	value*: prefix match
	*value: suffix match
	*value*:  contains match
	EXAMPLES
	type=Kubernetes::Namespace name=kube*
	type=AWS::EC2::Instance health=unhealthy
	type=Kubernetes::Deployment name=nginx*
	type=Kubernetes::* health=unhealthy
	created_at>now-24h
	updated_at>2025-01-01 updated_at<2025-01-31
	type=Kubernetes::Pod labelSelector="team in (payments,orders)"
	type=Kubernetes::Pod label.app=nginx tag.cluster=prod
	Use this single specification to parse requests, generate valid catalog-search queries, and validate existing ones.
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
	We can search all the catalog changes via query
	Use the tool: list_catalog_types to get all the types first to make inference is better (cache them for 15m)

	FORMAL PEG GRAMMAR:
	Query = AndQuery _ OrQuery*
	OrQuery = _ '|' _ AndQuery
	AndQuery = _ FieldQuery _ FieldQuery*
	FieldQuery = _ '(' _ Query _ ')' _
	/ _ Field _
	/ _ '-' Word _
	/ _ (Word / Identifier) _
	Field = Source _ Operator _ Value
	Source = Identifier ('.' Identifier)*
	Operator = "<=" / ">=" / "=" / ":" / "!=" / "<" / ">"
	Value = DateTime / ISODate / Time / Measure
	/ Float / Integer / Identifier / String
	String = '"' [^"]* '"'
	ISODate = [0-9]{4} '-' [0-9]{2} '-' [0-9]{2}
	Time = [0-2][0-9] ':' [0-5][0-9] ':' [0-5][0-9]
	DateTime = "now" (("+" / "-") Integer DurationUnit)?
	/ ISODate ? Time?
	DurationUnit = "s" / "m" / "h" / "d" / "w" / "mo" / "y"
	Word = String / '-'? [@a-zA-Z0-9-]+
	Integer = [+-]?[0-9]+ ![a-zA-Z0-9_-]
	Float = [+-]? [0-9] '.' [0-9]+
	Measure = (Integer / Float) Identifier
	Identifier = [@a-zA-Z0-9_\,-:\[\]]+
	_ = [ \t]
	EOF = !.
	QUERY SHAPE
	A query is one or more space-separated pairs: field=value.
	Order does not matter.
	Fields that match against the config item:  type | id | config_id | agent | name | namespace  | labelSelector | tagSelector | status | health | limit | created_at | updated_at | deleted_at | label.* | tag.*
	• label.* and tag.* accept any key after the dot, following Kubernetes label grammar.
	• labelSelector and tagSelector accept full Kubernetes selector expressions (e.g. env in (prod,stage), !deprecated).
	fields that match against the config items changes: change_type | severity | summary | count | first_observed
	type: <Provider>::<ConfigClass> or <Provider>::* (Providers: AWS, Azure, GCP, Kubernetes)
	health: healthy | unhealthy | warning | unknown
	severity: info,low,medium,high and critical
	status free text (wildcards allowed)
	limit: positive integer
	date fields compare with <, >, <=, >= against:
	– absolute ISO date YYYY-MM-DD
	– date-math now±N{ s | m | h | d | w | mo | y } (e.g. now-24h, now-7d)
	WILDCARDS
	value*: prefix match
	*value: suffix match
	*value*:  contains match
	EXAMPLES
	type=Kubernetes::Namespace name=kube*
	type=AWS::EC2::Instance health=unhealthy
	type=AWS::* severity=critical
	type=Kubernetes::Deployment name=nginx*
	type=Kubernetes::* health=unhealthy
	created_at>now-24h
	updated_at>2025-01-01 updated_at<2025-01-31
	type=Kubernetes::Pod labelSelector="team in (payments,orders)"
	type=Kubernetes::Pod label.app=nginx tag.cluster=prod
	Use this single specification to parse requests, generate valid catalog-search queries, and validate existing ones.
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
