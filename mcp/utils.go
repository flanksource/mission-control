package mcp

import (
	gocontext "context"
	"fmt"
	"regexp"
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

func structToMCPResponse(s any) *mcp.CallToolResult {
	md, err := clicky.Format(s, clicky.FormatOptions{Format: "markdown"})
	if err != nil {
		return mcp.NewToolResultError(err.Error())
	}
	return mcp.NewToolResultText(md)
}
