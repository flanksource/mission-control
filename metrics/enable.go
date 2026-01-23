package metrics

import (
	"strings"

	"github.com/flanksource/duty/context"
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
