package mcp

import (
	gocontext "context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/duty/context"
	"github.com/mark3labs/mcp-go/mcp"
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

type ArgParser struct {
	args map[string]any
}

func NewArgParser(raw any) ArgParser {
	if raw == nil {
		return ArgParser{args: map[string]any{}}
	}

	if m, ok := raw.(map[string]any); ok {
		return ArgParser{args: m}
	}

	return ArgParser{args: map[string]any{}}
}

func (p ArgParser) Raw() map[string]any {
	return p.args
}

func (p ArgParser) Bool(key string, defaultVal bool) bool {
	return getBoolArg(p.args, key, defaultVal)
}

func getBoolArg(args map[string]any, key string, defaultVal bool) bool {
	val, ok := args[key]
	if !ok {
		return defaultVal
	}

	switch v := val.(type) {
	case bool:
		return v
	case string:
		parsed, err := strconv.ParseBool(v)
		if err == nil {
			return parsed
		}
	}

	return defaultVal
}

func (p ArgParser) Int(key string, defaultVal int) int {
	return getIntArg(p.args, key, defaultVal)
}

func getIntArg(args map[string]any, key string, defaultVal int) int {
	val, ok := args[key]
	if !ok {
		return defaultVal
	}

	switch v := val.(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float64:
		return int(v)
	case string:
		parsed, err := strconv.Atoi(v)
		if err == nil {
			return parsed
		}
	}

	return defaultVal
}

func (p ArgParser) Strings(key string) []string {
	return getStringSliceArg(p.args, key)
}

func getStringSliceArg(args map[string]any, key string) []string {
	val, ok := args[key]
	if !ok || val == nil {
		return nil
	}

	switch v := val.(type) {
	case string:
		return splitAndTrim(v)
	case []string:
		return v
	case []any:
		var result []string
		for _, item := range v {
			result = append(result, fmt.Sprint(item))
		}
		return result
	}

	return nil
}

func splitAndTrim(value string) []string {
	parts := strings.Split(value, ",")
	var result []string
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func structToMCPResponse(s ...any) *mcp.CallToolResult {
	if len(s) == 0 {
		return mcp.NewToolResultText("")
	}

	var parts []string
	for _, item := range s {
		md, err := clicky.Format(item, clicky.FormatOptions{Format: "markdown"})
		if err != nil {
			return mcp.NewToolResultError(err.Error())
		}
		parts = append(parts, md)
	}

	return mcp.NewToolResultText(strings.Join(parts, "\n\n"))
}
