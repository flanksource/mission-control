package metrics

import (
	"strings"

	"github.com/flanksource/duty/context"
)

const metricsEnableProperty = "metrics.enable"

func metricEnabled(ctx context.Context, metric string) bool {
	enabled := ctx.Properties().String(metricsEnableProperty, "")
	if enabled == "" {
		return false
	}

	for _, entry := range strings.Split(enabled, ",") {
		value := strings.TrimSpace(entry)
		if value == "" {
			continue
		}
		if value == "*" || value == metric || value == "mission_control_"+metric {
			return true
		}
	}

	return false
}
