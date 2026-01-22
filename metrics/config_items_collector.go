package metrics

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

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
	mutex         sync.Mutex
	cachedAt      time.Time
	cachedItems   []configItemRow
}

type configItemRow struct {
	ID      uuid.UUID           `gorm:"column:id"`
	AgentID uuid.UUID           `gorm:"column:agent_id"`
	Name    *string             `gorm:"column:name"`
	Tags    types.JSONStringMap `gorm:"column:tags"`
	Health  *models.Health      `gorm:"column:health"`
}

const (
	configItemsCacheTTLProperty = "metrics.config_items.cache_ttl"
	defaultConfigItemsCacheTTL  = 5 * time.Minute
)

func newConfigItemsCollector(ctx context.Context, includeInfo, includeHealth bool) *configItemsCollector {
	collector := &configItemsCollector{
		ctx:           ctx,
		includeInfo:   includeInfo,
		includeHealth: includeHealth,
	}
	if includeInfo {
		collector.infoDesc = prometheus.NewDesc(prometheus.BuildFQName("mission_control", "", "config_items_info"), "Config item metadata.", []string{"id", "agent_id", "name", "namespace", "tags"}, nil)
	}
	if includeHealth {
		collector.healthDesc = prometheus.NewDesc(
			prometheus.BuildFQName("mission_control", "", "config_items_health"),
			"Config item health status (0=healthy, 1=warning, 2=error).",
			[]string{"config_id", "agent_id"},
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

	items, err := c.getCachedItems()
	if err != nil {
		c.ctx.Logger.Errorf("failed to collect config items: %v", err)
		return
	}

	for _, item := range items {
		agentID := formatAgentID(item.AgentID)
		if c.includeInfo {
			namespace := item.Tags["namespace"]
			ch <- prometheus.MustNewConstMetric(
				c.infoDesc,
				prometheus.GaugeValue,
				1,
				item.ID.String(),
				agentID,
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
				agentID,
			)
		}
	}
}

func formatAgentID(agentID uuid.UUID) string {
	if agentID == uuid.Nil {
		return ""
	}

	return agentID.String()
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

func (c *configItemsCollector) getCachedItems() ([]configItemRow, error) {
	cacheTTL := c.ctx.Properties().Duration(configItemsCacheTTLProperty, defaultConfigItemsCacheTTL)
	if cacheTTL < 0 {
		cacheTTL = 0
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	if len(c.cachedItems) > 0 && cacheTTL > 0 && time.Since(c.cachedAt) < cacheTTL {
		return c.cachedItems, nil
	}

	items, err := c.fetchConfigItems()
	if err != nil {
		if len(c.cachedItems) > 0 {
			c.ctx.Logger.Errorf("failed to refresh config items cache: %v", err)
			return c.cachedItems, nil
		}
		return nil, err
	}

	c.cachedItems = items
	c.cachedAt = time.Now()
	return items, nil
}

func (c *configItemsCollector) fetchConfigItems() ([]configItemRow, error) {
	columns := []string{"id", "agent_id"}
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
		return nil, err
	}
	defer rows.Close()

	items := make([]configItemRow, 0)
	for rows.Next() {
		var item configItemRow
		if err := c.ctx.DB().ScanRows(rows, &item); err != nil {
			c.ctx.Logger.Errorf("failed to scan config item row: %v", err)
			continue
		}
		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return items, err
	}

	return items, nil
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
