package mcp

import (
	gocontext "context"
	"fmt"
	"regexp"
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/duty/context"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func getDutyCtx(ctx gocontext.Context) (context.Context, error) {
	if v := ctx.Value(dutyContextKey); v != nil {
		dutyCtx, ok := v.(context.Context)
		if ok {
			return dutyCtx, nil
		}
	}
	return context.Context{}, fmt.Errorf("no duty ctx")
}

// fixMCPToolNameIfRequired removes invalid chars that do not
// match the MCP Tool Naming regex
func fixMCPToolNameIfRequired(s string) string {
	pattern := `^[a-zA-Z0-9_-]{1,64}$`
	re := regexp.MustCompile(pattern)

	if re.MatchString(s) {
		return s
	}

	// Replace invalid characters with underscores
	validChars := regexp.MustCompile(`[^a-zA-Z0-9_-]`)
	fixed := validChars.ReplaceAllString(s, "-")

	// Truncate if too long
	if len(fixed) > 64 {
		fixed = fixed[:64]
	}

	return fixed
}

func extractID(uri string) string {
	// Extract ID from "users://123" format
	parts := strings.Split(uri, "://")
	if len(parts) == 2 {
		return parts[1]
	}
	return ""
}

func extractNamespaceName(uri string) (string, string, error) {
	parts := strings.Split(uri, "://")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid format: %s", uri)
	}
	namespaceName := parts[1]
	parts = strings.Split(namespaceName, "/")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid format: %s", uri)
	}
	return parts[0], parts[1], nil
}

func structToMCPResponse(s any) *mcp.CallToolResult {
	md, err := clicky.Format(s, clicky.FormatOptions{Format: "markdown"})
	if err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				&mcp.TextContent{Text: err.Error()},
			},
		}
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: md},
		},
	}
}

// Helper functions to extract arguments from CallToolRequest
func requireString(req *mcp.CallToolRequest, key string) (string, error) {
	if req.Params.Arguments == nil {
		return "", fmt.Errorf("missing required parameter: %s", key)
	}
	val, ok := req.Params.Arguments[key]
	if !ok {
		return "", fmt.Errorf("missing required parameter: %s", key)
	}
	str, ok := val.(string)
	if !ok {
		return "", fmt.Errorf("parameter %s must be a string", key)
	}
	return str, nil
}

func getInt(req *mcp.CallToolRequest, key string, defaultValue int) int {
	if req.Params.Arguments == nil {
		return defaultValue
	}
	val, ok := req.Params.Arguments[key]
	if !ok {
		return defaultValue
	}
	// Try float64 first (JSON numbers are float64)
	if f, ok := val.(float64); ok {
		return int(f)
	}
	// Try int
	if i, ok := val.(int); ok {
		return i
	}
	return defaultValue
}

func getArguments(req *mcp.CallToolRequest) map[string]any {
	if req.Params.Arguments == nil {
		return make(map[string]any)
	}
	return req.Params.Arguments
}

func newToolResultError(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{
			&mcp.TextContent{Text: msg},
		},
	}
}

func newToolResultText(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: text},
		},
	}
}

// Helper to create a simple input schema for tools
func createInputSchema(properties map[string]any, required []string) any {
	schema := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}
