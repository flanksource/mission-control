package mcp

import (
	gocontext "context"
	"fmt"
	"net/url"

	"github.com/flanksource/commons/http"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/incident-commander/api"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/samber/lo"
)

func healthCheckSearchHandler(goctx gocontext.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return newToolResultError(err.Error()), nil
	}

	q, err := requireString(req, "query")
	if err != nil {
		return newToolResultError(err.Error()), nil
	}
	limit := getInt(req, "limit", 30)

	checks, err := query.FindChecks(ctx, limit, types.ResourceSelector{Search: q})
	if err != nil {
		return newToolResultError(err.Error()), nil
	}
	return structToMCPResponse(checks), nil
}

func checkStatusHandler(goctx gocontext.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return newToolResultError(err.Error()), nil
	}

	checkID, err := requireString(req, "id")
	if err != nil {
		return newToolResultError(err.Error()), nil
	}
	limit := getInt(req, "limit", 30)

	var checkStatuses []models.CheckStatus
	err = ctx.DB().Where("check_id = ?", checkID).Order("time DESC").Limit(limit).Find(&checkStatuses).Error
	if err != nil {
		return newToolResultError(err.Error()), nil
	}
	return structToMCPResponse(checkStatuses), nil
}

func healthCheckRunHandler(goctx gocontext.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return newToolResultError(err.Error()), nil
	}

	checkID, err := requireString(req, "id")
	if err != nil {
		return newToolResultError(err.Error()), nil
	}

	endpoint, err := url.JoinPath(api.CanaryCheckerPath, fmt.Sprintf("/run/check/%s", checkID))
	if err != nil {
		return newToolResultError(fmt.Sprintf("Failed to call health check URL: %v", err)), nil
	}

	resp, err := http.NewClient().R(ctx).Post(endpoint, nil)
	if err != nil {
		return newToolResultError(fmt.Sprintf("Failed to call health check URL: %v", err)), nil
	}

	if !resp.IsOK() {
		return newToolResultError(fmt.Sprintf("Failed to call health check URL: %v", err)), nil
	}

	body, err := resp.AsString()
	if err != nil {
		return newToolResultError(fmt.Sprintf("Failed to call health check URL: %v", err)), nil
	}

	return structToMCPResponse(body), nil

}

func listAllChecksHandler(goctx gocontext.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return newToolResultError(err.Error()), nil
	}

	checks, err := query.FindChecks(ctx, -1, types.ResourceSelector{Search: "limit=-1"})
	if err != nil {
		return newToolResultError(err.Error()), nil
	}
	return structToMCPResponse(checks), nil
}

func registerHealthChecks(s *mcp.Server) {
	var queryDescription = `
	We can search health checks via query using the same grammar as catalog_search
	Use the tool: health_check_search to find health checks based on search criteria

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
	Fields: id | name | namespace | canary_id | type | status | agent_id | created_at | updated_at | deleted_at | labels.* | spec.*
	• label.* and tag.* accept any key after the dot | following Kubernetes label grammar.
	status: healthy | unhealthy
	limit: positive integer
	date fields compare with <, >, <=, >= against:
	– absolute ISO date YYYY-MM-DD
	– date-math now±N{ s | m | h | d | w | mo | y } (e.g. now-24h, now-7d)
	WILDCARDS
	value*: prefix match
	*value: suffix match
	*value*:  contains match
	EXAMPLES
	name=api* status=unhealthy
	status=healthy labels.app=web
	created_at>now-24h
	updated_at>2025-01-01 updated_at<2025-01-31

	Use this single specification to parse requests, generate valid health check search queries, and validate existing ones.
`

	s.AddTool(&mcp.Tool{
		Name:        "search_health_checks",
		Description: "Search and find health checks returning JSON array with check metadata"),
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
	}, healthCheckSearchHandler)

	s.AddTool(&mcp.Tool{
		Name:        "get_check_status",
		Description: "Get health check execution history as JSON array. Each entry contains status, time, duration, and error (if any). Ordered by most recent first."),
		InputSchema: createInputSchema(map[string]any{
			"id": map[string]any{
				"type":        "string",
				"description": "Health check ID to get status history for",
			},
			"limit": map[string]any{
				"type":        "number",
				"description": "Number of status entries to return (default: 30)",
			},
		}, []string{"id"}),
	}, checkStatusHandler)

	s.AddTool(&mcp.Tool{
		Name:        "run_health_check",
		Description: "Execute a health check immediately and return results. Returns execution status and timing information."),
		InputSchema: createInputSchema(map[string]any{
			"id": map[string]any{
				"type":        "string",
				"description": "Health check ID to run",
			},
		}, []string{"id"}),
	}, healthCheckRunHandler)

	s.AddTool(&mcp.Tool{
		Name:        "list_all_checks",
		Description: "List all health checks as JSON array with complete metadata including names, IDs, and current status"),
	}, listAllChecksHandler)
}
