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
	tagKeys       []string
	tagLabelKeys  []string
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

var configItemInfoBaseLabels = []string{"id", "agent_id", "name", "namespace"}

func newConfigItemsCollector(ctx context.Context, includeInfo, includeHealth bool) *configItemsCollector {
	collector := &configItemsCollector{
		ctx:           ctx,
		includeInfo:   includeInfo,
		includeHealth: includeHealth,
	}
	if includeInfo {
		collector.infoDesc = nil
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

	for _, item := range items {
		agentID := item.AgentID.String()
		if c.includeInfo {
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
	labels = append(labels, item.ID.String(), agentID, lo.FromPtr(item.Name), namespace)
	for _, key := range c.tagKeys {
		labels = append(labels, item.Tags[key])
	}

	return labels
}

func (c *configItemsCollector) ensureInfoDescriptor(items []configItemRow) {
	if !c.includeInfo || c.infoDesc != nil {
		return
	}

	tagKeys, tagLabelKeys := buildTagLabels(items)
	c.tagKeys = tagKeys
	c.tagLabelKeys = tagLabelKeys

	labels := append(append([]string{}, configItemInfoBaseLabels...), tagLabelKeys...)
	c.infoDesc = prometheus.NewDesc(
		prometheus.BuildFQName("mission_control", "", "config_items_info"),
		"Config item metadata.",
		labels,
		nil,
	)
}

func buildTagLabels(items []configItemRow) ([]string, []string) {
	keysSet := make(map[string]struct{})
	for _, item := range items {
		for key := range item.Tags {
			if key == "" {
				continue
			}
			keysSet[key] = struct{}{}
		}
	}

	tagKeys := make([]string, 0, len(keysSet))
	for key := range keysSet {
		tagKeys = append(tagKeys, key)
	}
	sort.Strings(tagKeys)

	labelKeys := make([]string, 0, len(tagKeys))
	labelUseCounts := make(map[string]int)
	for _, key := range tagKeys {
		label := sanitizeTagLabel(key)
		if count, exists := labelUseCounts[label]; exists {
			count++
			labelUseCounts[label] = count
			label = fmt.Sprintf("%s_%d", label, count)
		} else {
			labelUseCounts[label] = 0
		}
		labelKeys = append(labelKeys, label)
	}

	return tagKeys, labelKeys
}

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

	return "tag_" + sanitized
}

func (c *configItemsCollector) getCachedItems() ([]configItemRow, error) {
	cacheTTL := c.ctx.Properties().Duration(configItemsCacheTTLProperty, defaultConfigItemsCacheTTL)
	if cacheTTL < 0 {
		cacheTTL = 0
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	if len(c.cachedItems) > 0 && cacheTTL > 0 && time.Since(c.cachedAt) < cacheTTL {
		c.ensureInfoDescriptor(c.cachedItems)
		return c.cachedItems, nil
	}

	items, err := c.fetchConfigItems()
	if err != nil {
		if len(c.cachedItems) > 0 {
			c.ctx.Logger.Errorf("failed to refresh config items cache: %v", err)
			c.ensureInfoDescriptor(c.cachedItems)
			return c.cachedItems, nil
		}
		return nil, err
	}

	c.ensureInfoDescriptor(items)
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
