package mcp

import (
	"regexp"
	"strings"
)

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
