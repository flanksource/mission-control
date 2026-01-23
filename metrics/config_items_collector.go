package metrics

import (
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
	tagKeys       []string
	tagLabelKeys  []string
}

type configItemRow struct {
	ID      uuid.UUID           `gorm:"column:id"`
	AgentID uuid.UUID           `gorm:"column:agent_id"`
	Name    *string             `gorm:"column:name"`
	Type    *string             `gorm:"column:type"`
	Tags    types.JSONStringMap `gorm:"column:tags"`
	Health  *models.Health      `gorm:"column:health"`
}

const (
	configItemsCacheTTLProperty = "metrics.config_items.cache_ttl"
	defaultConfigItemsCacheTTL  = 5 * time.Minute
)

var configItemInfoBaseLabels = []string{"id", "agent_id", "name", "type", "namespace"}

func newConfigItemsCollector(ctx context.Context, includeInfo, includeHealth bool) *configItemsCollector {
	collector := &configItemsCollector{
		ctx:           ctx,
		includeInfo:   includeInfo,
		includeHealth: includeHealth,
	}
	if includeHealth {
		collector.healthDesc = prometheus.NewDesc(
			prometheus.BuildFQName("mission_control", "", "config_items_health"),
			"Config item health status (0=healthy, 1=warning, 2=unhealthy, 3=unknown).",
			[]string{"id", "agent_id"},
			nil,
		)
	}
	return collector
}

func (c *configItemsCollector) Describe(ch chan<- *prometheus.Desc) {
	if c.includeInfo {
		if c.infoDesc == nil {
			if _, err := c.getCachedItems(); err != nil {
				c.ctx.Logger.Errorf("failed to load config items for descriptor: %v", err)
			}
		}
		if c.infoDesc != nil {
			ch <- c.infoDesc
		}
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

	infoReady := c.includeInfo && c.infoDesc != nil
	if c.includeInfo && c.infoDesc == nil {
		c.ctx.Logger.Errorf("config items info metric disabled: label descriptor not available")
	}

	for _, item := range items {
		agentID := formatUUID(item.AgentID)
		if infoReady {
			labels := c.infoLabelValues(item, agentID)
			ch <- prometheus.MustNewConstMetric(
				c.infoDesc,
				prometheus.GaugeValue,
				1,
				labels...,
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

func (c *configItemsCollector) infoLabelValues(item configItemRow, agentID string) []string {
	labels := make([]string, 0, len(configItemInfoBaseLabels)+len(c.tagKeys))
	namespace := item.Tags["namespace"]
	labels = append(labels, item.ID.String(), agentID, lo.FromPtr(item.Name), lo.FromPtr(item.Type), namespace)
	for _, key := range c.tagKeys {
		labels = append(labels, item.Tags[key])
	}

	return labels
}

func (c *configItemsCollector) ensureInfoDescriptor() {
	if !c.includeInfo || c.infoDesc != nil {
		return
	}

	if err := c.loadTagLabels(); err != nil {
		c.ctx.Logger.Errorf("failed to load config tag keys: %v", err)
		return
	}

	labels := append([]string(nil), configItemInfoBaseLabels...)
	labels = append(labels, c.tagLabelKeys...)
	c.infoDesc = prometheus.NewDesc(
		prometheus.BuildFQName("mission_control", "", "config_items_info"),
		"Config item metadata.",
		labels,
		nil,
	)
}

func (c *configItemsCollector) loadTagLabels() error {
	var tagKeys []string
	if err := c.ctx.DB().Table("config_tags").Select("key").Distinct().Order("key").Pluck("key", &tagKeys).Error; err != nil {
		return err
	}

	usedLabels := make(map[string]struct{}, len(configItemInfoBaseLabels)+len(tagKeys))
	for _, label := range configItemInfoBaseLabels {
		usedLabels[label] = struct{}{}
	}

	labelKeys := make([]string, 0, len(tagKeys))
	for _, key := range tagKeys {
		label := sanitizeTagLabel(key)
		if _, exists := usedLabels[label]; exists {
			label = "tag_" + label
		}
		label = ensureUniqueLabel(label, usedLabels)
		labelKeys = append(labelKeys, label)
	}

	c.tagKeys = tagKeys
	c.tagLabelKeys = labelKeys
	return nil
}

func (c *configItemsCollector) getCachedItems() ([]configItemRow, error) {
	cacheTTL := c.ctx.Properties().Duration(configItemsCacheTTLProperty, defaultConfigItemsCacheTTL)
	if cacheTTL < 0 {
		cacheTTL = 0
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	hasCache := !c.cachedAt.IsZero()
	if hasCache && cacheTTL > 0 && time.Since(c.cachedAt) < cacheTTL {
		c.ensureInfoDescriptor()
		return c.cachedItems, nil
	}

	items, err := c.fetchConfigItems()
	if err != nil {
		if hasCache {
			c.ctx.Logger.Errorf("failed to refresh config items cache: %v", err)
			c.ensureInfoDescriptor()
			return c.cachedItems, nil
		}
		return nil, err
	}

	c.cachedItems = items
	c.cachedAt = time.Now()
	c.ensureInfoDescriptor()
	return items, nil
}

func (c *configItemsCollector) fetchConfigItems() ([]configItemRow, error) {
	columns := []string{"id", "agent_id"}
	if c.includeInfo {
		columns = append(columns, "name", "type", "tags")
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
		return 3
	}

	switch *health {
	case models.HealthHealthy:
		return 0
	case models.HealthWarning:
		return 1
	case models.HealthUnhealthy:
		return 2
	case models.HealthUnknown:
		return 3
	default:
		return 3
	}
}
