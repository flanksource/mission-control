package metrics

import (
	"fmt"
	"sort"
	"strings"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/samber/lo"
)

type configItemsCollector struct {
	ctx           context.Context
	includeInfo   bool
	includeHealth bool
	infoDesc      *prometheus.Desc
	healthDesc    *prometheus.Desc
}

type configItemRow struct {
	ID     uuid.UUID           `gorm:"column:id"`
	Name   *string             `gorm:"column:name"`
	Tags   types.JSONStringMap `gorm:"column:tags"`
	Health *models.Health      `gorm:"column:health"`
}

func newConfigItemsCollector(ctx context.Context, includeInfo, includeHealth bool) *configItemsCollector {
	collector := &configItemsCollector{
		ctx:           ctx,
		includeInfo:   includeInfo,
		includeHealth: includeHealth,
	}
	if includeInfo {
		collector.infoDesc = prometheus.NewDesc(prometheus.BuildFQName("mission_control", "", "config_items_info"), "Config item metadata.", []string{"id", "name", "namespace", "tags"}, nil)
	}
	if includeHealth {
		collector.healthDesc = prometheus.NewDesc(
			prometheus.BuildFQName("mission_control", "", "config_items_health"),
			"Config item health status (0=healthy, 1=warning, 2=error).",
			[]string{"config_id"},
			nil,
		)
	}
	return collector
}

func (c *configItemsCollector) Describe(ch chan<- *prometheus.Desc) {
	if c.includeInfo {
		ch <- c.infoDesc
	}
	if c.includeHealth {
		ch <- c.healthDesc
	}
}

func (c *configItemsCollector) Collect(ch chan<- prometheus.Metric) {
	if !c.includeInfo && !c.includeHealth {
		return
	}

	columns := []string{"id"}
	if c.includeInfo {
		columns = append(columns, "name", "tags")
	}
	if c.includeHealth {
		columns = append(columns, "health")
	}

	rows, err := c.ctx.DB().Model(&models.ConfigItem{}).
		Select(columns).
		Where("deleted_at IS NULL").
		Rows()
	if err != nil {
		c.ctx.Logger.Errorf("failed to collect config items: %v", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var item configItemRow
		if err := c.ctx.DB().ScanRows(rows, &item); err != nil {
			c.ctx.Logger.Errorf("failed to scan config item row: %v", err)
			continue
		}

		if c.includeInfo {
			namespace := item.Tags["namespace"]
			ch <- prometheus.MustNewConstMetric(
				c.infoDesc,
				prometheus.GaugeValue,
				1,
				item.ID.String(),
				lo.FromPtr(item.Name),
				namespace,
				formatConfigItemTags(item.Tags),
			)
		}

		if c.includeHealth {
			ch <- prometheus.MustNewConstMetric(
				c.healthDesc,
				prometheus.GaugeValue,
				configItemHealthValue(item.Health),
				item.ID.String(),
			)
		}
	}

	if err := rows.Err(); err != nil {
		c.ctx.Logger.Errorf("failed to iterate config items: %v", err)
	}
}

func formatConfigItemTags(tags types.JSONStringMap) string {
	if len(tags) == 0 {
		return ""
	}

	keys := make([]string, 0, len(tags))
	for key := range tags {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, fmt.Sprintf("%s=%s", key, tags[key]))
	}

	return strings.Join(pairs, ",")
}

func configItemHealthValue(health *models.Health) float64 {
	if health == nil {
		return 2
	}

	switch *health {
	case models.HealthHealthy:
		return 0
	case models.HealthWarning:
		return 1
	case models.HealthUnhealthy:
		return 2
	default:
		return 2
	}
}
