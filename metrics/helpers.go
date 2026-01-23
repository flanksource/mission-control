// Package metrics provides Prometheus metrics collectors.
// This file contains shared helper functions used across multiple collectors.
package metrics

import (
	"fmt"
	"strings"

	"github.com/flanksource/duty/context"
	"github.com/google/uuid"
)

const metricsDisableProperty = "metrics.disable"

func metricEnabled(ctx context.Context, metric string) bool {
	disabled := ctx.Properties().String(metricsDisableProperty, "")
	if disabled == "" {
		return true
	}

	for _, entry := range strings.Split(disabled, ",") {
		value := strings.TrimSpace(entry)
		if value == "" {
			continue
		}
		if value == "*" || value == metric || value == "mission_control_"+metric {
			return false
		}
	}

	return true
}

// ensureUniqueLabel ensures that the label name is unique by appending a suffix if needed.
// Used by checks_collector.go, config_items_collector.go, and scrapers_collector.go.
func ensureUniqueLabel(base string, used map[string]struct{}) string {
	label := base
	for idx := 0; ; idx++ {
		if _, exists := used[label]; !exists {
			used[label] = struct{}{}
			return label
		}
		label = fmt.Sprintf("%s_%d", base, idx+1)
	}
}

// formatUUID returns the string representation of a UUID, or empty string if nil.
// Used by checks_collector.go, config_items_collector.go, and agents_collector.go.
func formatUUID(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}

// sanitizeTagLabel converts a tag key to a valid Prometheus label name.
// Used by checks_collector.go, config_items_collector.go, and scrapers_collector.go.
func sanitizeTagLabel(key string) string {
	var builder strings.Builder
	for _, char := range key {
		switch {
		case char >= 'a' && char <= 'z':
			builder.WriteRune(char)
		case char >= 'A' && char <= 'Z':
			builder.WriteRune(char)
		case char >= '0' && char <= '9':
			builder.WriteRune(char)
		case char == '_':
			builder.WriteRune(char)
		default:
			builder.WriteRune('_')
		}
	}

	sanitized := builder.String()
	if sanitized == "" {
		sanitized = "tag"
	}

	if sanitized[0] >= '0' && sanitized[0] <= '9' {
		sanitized = "tag_" + sanitized
	}

	return sanitized
}
