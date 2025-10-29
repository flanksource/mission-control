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
	"github.com/flanksource/incident-commander/db"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/samber/lo"
)

//go:embed k8s_troubleshooting_prompt.txt
var k8sTroubleshootingPrompt string

func searchCatalogHandler(goctx gocontext.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return newToolResultError(err.Error()), nil
	}

	q, err := requireString(req, "query")
	if err != nil {
		return newToolResultError(err.Error()), nil
	}
	limit := getInt(req, "limit", 30)

	var cis any
	switch req.Params.Name {
	case "describe_config":
		cis, err = queryConfigItemDescription(ctx, limit, q)
	default:
		cis, err = queryConfigItemSummary(ctx, limit, q)
	}

	if err != nil {
		return newToolResultError(err.Error()), nil
	}

	return structToMCPResponse(cis), nil
}

type ConfigDescription struct {
	models.ConfigItem
	AvailableTools []string `json:"available_tools"`
}

func queryConfigItemSummary(ctx context.Context, limit int, q string) ([]models.ConfigItemSummary, error) {
	return query.FindConfigItemSummaryByResourceSelector(ctx, limit, types.ResourceSelector{Search: q})
}

func queryConfigItemDescription(ctx context.Context, limit int, q string) ([]ConfigDescription, error) {
	configs, err := query.FindConfigsByResourceSelector(ctx, limit, types.ResourceSelector{Search: q})
	if err != nil {
		return nil, err
	}
	var cds []ConfigDescription
	for _, c := range configs {
		_, pbs, err := db.FindPlaybooksForConfig(ctx, c)
		if err != nil {
			return nil, err
		}

		cds = append(cds, ConfigDescription{
			ConfigItem: c,
			AvailableTools: lo.Map(pbs, func(p *models.Playbook, _ int) string {
				return generatePlaybookToolName(lo.FromPtr(p))
			}),
		})
	}
	return cds, nil
}

func searchConfigChangesHandler(goctx gocontext.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	q, err := requireString(req, "query")
	if err != nil {
		return newToolResultError(err.Error()), nil
	}

	limit := getInt(req, "limit", 30)

	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return newToolResultError(err.Error()), nil
	}
	cis, err := query.FindConfigChangesByResourceSelector(ctx, limit, types.ResourceSelector{Search: q})
	if err != nil {
		return newToolResultError(err.Error()), nil
	}
	return structToMCPResponse(cis), nil
}

func relatedCatalogHandler(goctx gocontext.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	rawID, err := requireString(req, "id")
	if err != nil {
		return newToolResultError(err.Error()), nil
	}

	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return newToolResultError(err.Error()), nil
	}
	id, err := uuid.Parse(rawID)
	if err != nil {
		return newToolResultError(err.Error()), nil
	}

	cis, err := query.GetRelatedConfigs(ctx, query.RelationQuery{
		ID: id,
	})
	if err != nil {
		return newToolResultError(err.Error()), nil
	}
	return structToMCPResponse(cis), nil
}

func configTypeResourceHandler(goctx gocontext.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return nil, err
	}

	var types []string
	err = ctx.DB().Model(&models.ConfigItem{}).Select("DISTINCT(type)").Find(&types).Error
	if err != nil {
		return newToolResultError(err.Error()), nil
	}
	return newToolResultText(strings.Join(types, "\n")), nil
}

func ConfigItemResourceHandler(goctx gocontext.Context, req *mcp.ReadResourceRequest) ([]*mcp.ResourceContents, error) {
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

	return []*mcp.ResourceContents{
		{
			URI:      req.Params.URI,
			MIMEType: "application/json",
			Text:     string(jsonData),
		},
	}, nil
}

func unhealthyCatalogItemsPromptHandler(ctx gocontext.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	return &mcp.GetPromptResult{
		Description: "Search for unhealthy catalog items",
		Messages: []*mcp.PromptMessage{
			{
				Role: mcp.RoleUser,
				Content: mcp.Content{
					&mcp.TextContent{
						Text: "Query the catalog using search_catalog tool for all unhealthy items with the query: health!=healthy",
					},
				},
			},
		},
	}, nil
}

func troubleshootKubernetesErrorPrompt(ctx gocontext.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	q := lo.CoalesceOrEmpty(req.Params.Arguments["query"], "health!=healthy type=Kubernetes::*")
	return &mcp.GetPromptResult{
		Description: "Troubleshoot kubernetes resources",
		Messages: []*mcp.PromptMessage{
			{
				Role: mcp.RoleUser,
				Content: mcp.Content{
					&mcp.TextContent{
						Text: fmt.Sprintf(k8sTroubleshootingPrompt, q),
					},
				},
			},
		},
	}, nil
}

func registerCatalog(s *mcp.Server) {
	s.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: "config_item://{id}",
		Name:        "Config Item",
		Description: "Config Item Data",
		MIMEType:    "application/json",
	}, ConfigItemResourceHandler)

	s.AddTool(&mcp.Tool{
		Name:        "list_catalog_types",
		Description: "List all config types",
	}, configTypeResourceHandler)

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

	catalogSearchDescription := `
	Each catalog item also has more information in its config field which can be queried by calling the tool describe_config(query), the query is the same
	but that tool should only be called when "describe" is explicitly used
	`

	s.AddTool(&mcp.Tool{
		Name:        "search_catalog",
		Description: "Search and find configuration items in the catalog. For detailed config data, use describe_config tool." + catalogSearchDescription,
		InputSchema: createInputSchema(map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Search query." + queryDescription,
			},
			"limit": map[string]any{
				"type":        "number",
				"description": "Number of items to return",
			},
		}, []string{"query"}),
	}, searchCatalogHandler)

	describeConfigDescription := `
	This tool should only be called when "describe" is explicitly used, for all other purposes search_catalog tool should be used.
	Ideally, when prompted to describe configs use either the previous query or the config ids in the query field in csv format.

	Each config item returned will have a field "available_tools", which refers to all the existing tools in the current mcp server. We can call
	those tools with the param config_id=<id> and ask the user for any other parameters if the input schema requires any.

	Example query: id=f47ac10b-58cc-4372-a567-0e02b2c3d479,6ba7b810-9dad-11d1-80b4-00c04fd430c8,a1b2c3d4-e5f6-7890-abcd-ef1234567890
	`

	s.AddTool(&mcp.Tool{
		Name:        "describe_config",
		Description: "Get all data for configs." + describeConfigDescription,
		InputSchema: createInputSchema(map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Search query." + queryDescription,
			},
			"limit": map[string]any{
				"type":        "number",
				"description": "Number of items to return",
			},
		}, []string{"query"}),
	}, searchCatalogHandler)

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

	s.AddTool(&mcp.Tool{
		Name:        "search_catalog_changes",
		Description: "Search and find configuration change events across catalog items",
		InputSchema: createInputSchema(map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Search query" + configChangeQueryDescription,
			},
			"limit": map[string]any{
				"type":        "number",
				"description": "Number of results to return",
			},
		}, []string{"query"}),
	}, searchConfigChangesHandler)

	s.AddTool(&mcp.Tool{
		Name:        "get_related_configs",
		Description: "Find configuration items related to a specific config by relationships and dependencies",
		InputSchema: createInputSchema(map[string]any{
			"id": map[string]any{
				"type":        "string",
				"description": "Config ID",
			},
		}, []string{"id"}),
	}, relatedCatalogHandler)

	s.AddPrompt(&mcp.Prompt{
		Name: "Unhealthy catalog items",
	}, unhealthyCatalogItemsPromptHandler)

	s.AddPrompt(&mcp.Prompt{
		Name: "troubleshoot_kubernetes_resource",
		Arguments: []*mcp.PromptArgument{
			{
				Name:        "query",
				Description: "query to use for fetching catalog items to troubleshoot"),
				Required:    false,
			},
		},
	}, troubleshootKubernetesErrorPrompt)
}
