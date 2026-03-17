package mcp

import (
	gocontext "context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/samber/lo"

	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/db"
)

//go:embed k8s_troubleshooting_prompt.txt
var k8sTroubleshootingPrompt string

const (
	defaultQueryLimit = 30

	toolListCatalogTypes = "list_catalog_types"
	toolSearchCatalog    = "search_catalog"
	toolDescribeCatalog  = "describe_catalog"

	toolSearchCatalogChanges = "search_catalog_changes"

	toolGetRelatedConfigs = "get_related_configs"
)

var (
	defaultSelectConfigsView       = []string{"id", "name", "type", "health", "status", "description", "updated_at", "created_at"}
	defaultSelectConfigChangesView = []string{"id", "config_id", "name", "type", "change_type", "severity", "summary", "created_at", "first_observed", "count"}
)

func searchCatalogHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	q, err := req.RequireString("query")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	limit := req.GetInt("limit", defaultQueryLimit)

	var cis any
	err = auth.WithRLS(ctx, func(rlsCtx context.Context) error {
		selectCols := req.GetStringSlice("select", defaultSelectConfigsView)

		// NOTE: we're reading QueryTableColumnsWithResourceSelectors into map[string]any instead of []models.ConfigItemSummary
		// because, with a select clause with few columns, we only want to print those fields as columns in the markdown table.
		//
		// If we read into []models.ConfigItemSummary, clicky produces a column for every field in the struct, even if they are not selected.
		// https://github.com/flanksource/clicky/issues/40
		cis, err = query.QueryTableColumnsWithResourceSelectors[map[string]any](
			rlsCtx, "configs", selectCols, limit, nil, types.ResourceSelector{Search: q},
		)
		return err
	})

	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	return structToMCPResponse(req, cis), nil
}

type ConfigDescription struct {
	models.ConfigItem
	AvailableTools []string `json:"available_tools"`
}

func describeConfigHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	rawID, err := req.RequireString("id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if _, err := uuid.Parse(rawID); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	var config *models.ConfigItem
	var availableTools []string
	err = auth.WithRLS(ctx, func(rlsCtx context.Context) error {
		config, err = query.GetCachedConfig(rlsCtx, rawID)
		if err != nil || config == nil {
			return err
		}

		_, pbs, err := db.FindPlaybooksForConfig(rlsCtx, *config)
		if err != nil {
			return err
		}

		availableTools = lo.Map(pbs, func(p *models.Playbook, _ int) string {
			return generatePlaybookToolName(lo.FromPtr(p))
		})

		return nil
	})

	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if config == nil {
		return mcp.NewToolResultError(fmt.Sprintf("config item[%s] not found", rawID)), nil
	}

	return structToMCPResponse(req, []ConfigDescription{{
		ConfigItem:     *config,
		AvailableTools: availableTools,
	}}), nil
}

func searchConfigChangesHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	q, err := req.RequireString("query")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	limit := req.GetInt("limit", defaultQueryLimit)

	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	selectCols := req.GetStringSlice("select", defaultSelectConfigChangesView)

	var cis []map[string]any
	err = auth.WithRLS(ctx, func(rlsCtx context.Context) error {
		// NOTE: we're reading QueryTableColumnsWithResourceSelectors into map[string]any instead of []models.CatalogChange
		// because, with a select clause with few columns, we only want to print those fields as columns in the markdown table.
		//
		// If we read into []models.CatalogChange, clicky produces a column for every field in the struct, even if they are not selected.
		// This is especially important for config_changes as it includes heavy JSON fields like "config" and "details".
		// https://github.com/flanksource/clicky/issues/40
		cis, err = query.QueryTableColumnsWithResourceSelectors[map[string]any](
			rlsCtx, "catalog_changes", selectCols, limit, nil, types.ResourceSelector{Search: q},
		)
		return err
	})

	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return structToMCPResponse(req, cis), nil
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

	var cis []query.RelatedConfig
	err = auth.WithRLS(ctx, func(rlsCtx context.Context) error {
		cis, err = query.GetRelatedConfigs(rlsCtx, query.RelationQuery{
			ID: id,
		})
		return err
	})

	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return structToMCPResponse(req, cis), nil
}

func configTypeResourceHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return nil, err
	}

	var types []string
	err = auth.WithRLS(ctx, func(rlsCtx context.Context) error {
		return rlsCtx.DB().Model(&models.ConfigItem{}).Select("DISTINCT(type)").Find(&types).Error
	})

	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(strings.Join(types, "\n")), nil
}

func ConfigItemResourceHandler(goctx gocontext.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	id := extractID(req.Params.URI)

	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return nil, err
	}

	var ci *models.ConfigItem
	err = auth.WithRLS(ctx, func(rlsCtx context.Context) error {
		ci, err = query.GetCachedConfig(rlsCtx, id)
		return err
	})

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

func unhealthyCatalogItemsPromptHandler(ctx gocontext.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	return &mcp.GetPromptResult{
		Description: "Search for unhealthy catalog items",
		Messages: []mcp.PromptMessage{
			{
				Role:    "user",
				Content: mcp.NewTextContent("Query the catalog using search_catalog tool for all unhealthy items with the query: health!=healthy"),
			},
		},
	}, nil
}

func troubleshootKubernetesErrorPrompt(ctx gocontext.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	q := lo.CoalesceOrEmpty(req.Params.Arguments["query"], "health!=healthy type=Kubernetes::*")
	return &mcp.GetPromptResult{
		Description: "Troubleshoot kubernetes resources",
		Messages: []mcp.PromptMessage{
			{
				Role:    "user",
				Content: mcp.NewTextContent(fmt.Sprintf(k8sTroubleshootingPrompt, q)),
			},
		},
	}, nil
}

func registerCatalog(s *server.MCPServer) {
	s.AddResourceTemplate(
		mcp.NewResourceTemplate("config_item://{id}", "Config Item",
			mcp.WithTemplateDescription("Config Item Data"), mcp.WithTemplateMIMEType(echo.MIMEApplicationJSON)),
		ConfigItemResourceHandler)

	s.AddTool(mcp.NewTool(toolListCatalogTypes,
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

	catalogSearchDescription := fmt.Sprintf(`
	Each catalog item also has more information in its config field which can be retrieved by calling a different tool: %s(id).
	Use the id from search results; %s only accepts a single config id and should be called when "describe" is explicitly used.

	IMPORTANT - Column Selection for Token Efficiency:
	ALWAYS specify the "select" parameter with only the columns you need to minimize token usage.
	Default columns (id,name,type,health,status,description,updated_at,created_at) provide essential metadata.

	Available columns for ConfigItemSummary:
	- Lightweight: id, name, type, status, health, description, created_at, updated_at, deleted_at, scraper_id, agent_id, external_id, source, path, ready, cost_per_minute, cost_total_1d, cost_total_7d, cost_total_30d, delete_reason, labels, tags, namespace, changes, analysis, created_by
	- Note: ConfigItemSummary does NOT include config or properties fields. For full config data, use %s tool.

	Examples:
	- For basic listing: "id,name,type,health,status"
	- For troubleshooting: "id,name,type,health,status,description,changes"
	- For cost analysis: "id,name,type,cost_per_minute,cost_total_30d"
	`, toolDescribeCatalog, toolDescribeCatalog, toolDescribeCatalog)

	searchCatalogTool := mcp.NewTool(toolSearchCatalog,
		mcp.WithDescription(fmt.Sprintf("Search and find configuration items (not health checks) in the catalog. For detailed config data, use %s tool. %s", toolDescribeCatalog, catalogSearchDescription)),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("Search query."+queryDescription),
		),
		mcp.WithNumber("limit", mcp.Description(fmt.Sprintf("Number of items to return. Default: %d", defaultQueryLimit))),
		mcp.WithArray("select",
			mcp.WithStringItems(),
			mcp.Description("a list of columns to return. Default: id,name,type,health,status,description,updated_at,created_at. Always specify minimal columns needed for token efficiency."),
		),
	)
	s.AddTool(searchCatalogTool, searchCatalogHandler)

	describeConfigDescription := `
	Describe tool returns detailed metadata of a config item.
	Provide a single config item id (UUID) from search_catalog results to fetch the full record.

	Each config item returned will have a field "available_tools", which refers to all the existing tools in the current mcp server.
	We can call those tools with the param config_id=<id> and ask the user for any other parameters if the input schema requires any.

	NOTE: This tool is explicitly for config items and not for health checks.
	`
	s.AddTool(mcp.NewTool(toolDescribeCatalog,
		mcp.WithDescription(fmt.Sprintf("Get all data for configs. %s", describeConfigDescription)),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithString("id",
			mcp.Required(),
			mcp.Description("Config item id (UUID)"),
		),
	), describeConfigHandler)

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

	catalogChangesDescription := `
	IMPORTANT - Column Selection for Token Efficiency:
	ALWAYS specify the "select" parameter with only the columns you need to minimize token usage.
	Default columns (id,config_id,name,type,change_type,severity,summary,created_at,first_observed,count) provide essential change metadata.

	Available columns for CatalogChange:
	- Lightweight: id, config_id, name, type, change_type, severity, summary, created_at, first_observed, count, external_created_by, created_by, source, deleted_at, agent_id, tags
	- Heavy (avoid unless needed): config, details, diff - these are large JSON fields containing full configuration data, change details, and diffs

	Examples:
	- For basic change listing: "id,config_id,name,type,change_type,severity,created_at"
	- For change analysis: "id,config_id,change_type,severity,summary,first_observed,count"
	- For critical changes: "id,config_id,name,change_type,severity,summary,source"
	- Only when full details needed: "id,config_id,change_type,severity,summary,details"
	`

	searchCatalogChangesTool := mcp.NewTool(toolSearchCatalogChanges,
		mcp.WithDescription("Search and find configuration change events across catalog items"+catalogChangesDescription),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("Search query"+configChangeQueryDescription),
		),
		mcp.WithNumber("limit", mcp.Description(fmt.Sprintf("Number of results to return. Default: %d", defaultQueryLimit))),
		mcp.WithArray("select",
			mcp.WithStringItems(),
			mcp.Description("a list of columns to return. Default: id,config_id,name,type,change_type,severity,summary,created_at,first_observed,count. Always specify minimal columns needed for token efficiency. Avoid 'config', 'details', and 'diff' columns unless absolutely necessary as they contain large JSON data."),
		),
	)
	s.AddTool(searchCatalogChangesTool, searchConfigChangesHandler)

	relatedCatalogTool := mcp.NewTool(toolGetRelatedConfigs,
		mcp.WithDescription("Find configuration items related to a specific config by relationships and dependencies"),
		mcp.WithString("id",
			mcp.Required(),
			mcp.Description("Config ID"),
		),
	)
	s.AddTool(relatedCatalogTool, relatedCatalogHandler)

	s.AddPrompt(mcp.NewPrompt("Unhealthy catalog items"), unhealthyCatalogItemsPromptHandler)
	s.AddPrompt(mcp.NewPrompt("troubleshoot_kubernetes_resource",
		mcp.WithArgument("query", mcp.ArgumentDescription("query to use for fetching catalog items to troubleshoot")),
	), troubleshootKubernetesErrorPrompt)
}
