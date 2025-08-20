package mcp

import (
	gocontext "context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/flanksource/commons/http"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/incident-commander/api"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func healthCheckSearchHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	q, err := req.RequireString("query")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	limit := req.GetInt("limit", 30)

	checks, err := query.FindChecks(ctx, limit, types.ResourceSelector{Search: q})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	jsonData, err := json.Marshal(checks)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	return mcp.NewToolResultText(string(jsonData)), nil
}

func checkStatusHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	checkID, err := req.RequireString("id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	limit := req.GetInt("limit", 30)

	var checkStatuses []models.CheckStatus
	err = ctx.DB().Where("check_id = ?", checkID).Order("time DESC").Limit(limit).Find(&checkStatuses).Error
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	jsonData, err := json.Marshal(checkStatuses)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	return mcp.NewToolResultText(string(jsonData)), nil
}

func healthCheckRunHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	checkID, err := req.RequireString("id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	endpoint, err := url.JoinPath(api.CanaryCheckerPath, fmt.Sprintf("/run/check/%s", checkID))
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to call health check URL: %v", err)), nil
	}

	resp, err := http.NewClient().R(ctx).Post(endpoint, nil)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to call health check URL: %v", err)), nil
	}

	if !resp.IsOK() {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to call health check URL: %v", err)), nil
	}

	body, err := resp.AsString()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to call health check URL: %v", err)), nil
	}

	return mcp.NewToolResultText(body), nil
}

func listAllChecksHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	checks, err := query.FindChecks(ctx, -1, types.ResourceSelector{Search: "limit=-1"})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	jsonData, err := json.Marshal(checks)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	return mcp.NewToolResultText(string(jsonData)), nil
}

func registerHealthChecks(s *server.MCPServer) {
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

	healthCheckSearchTool := mcp.NewTool("health_check_search",
		mcp.WithDescription("Search across health checks"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("Search query."+queryDescription),
		),
		mcp.WithNumber("limit", mcp.Description("Number of items to return")),
	)
	s.AddTool(healthCheckSearchTool, healthCheckSearchHandler)

	getCheckStatusTool := mcp.NewTool("get_check_status",
		mcp.WithDescription("Get status history for a specific health check"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithString("id",
			mcp.Required(),
			mcp.Description("Health check ID to get status history for"),
		),
		mcp.WithNumber("limit", mcp.Description("Number of status entries to return (default: 30)")),
	)
	s.AddTool(getCheckStatusTool, checkStatusHandler)

	healthCheckRunTool := mcp.NewTool("health_check_run",
		mcp.WithDescription("Run any health check instantly"),
		mcp.WithString("id",
			mcp.Required(),
			mcp.Description("Health check ID to run"),
		),
	)
	s.AddTool(healthCheckRunTool, healthCheckRunHandler)

	listAllChecksTool := mcp.NewTool("list_all_checks",
		mcp.WithDescription("List all health checks"),
		mcp.WithReadOnlyHintAnnotation(true),
	)
	s.AddTool(listAllChecksTool, listAllChecksHandler)
}
