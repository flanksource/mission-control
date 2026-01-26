package metrics

import (
	"strings"
	"sync"
	"time"

	"github.com/flanksource/commons/collections"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
)

type checksCollector struct {
	ctx           context.Context
	includeInfo   bool
	includeHealth bool
	infoDesc      *prometheus.Desc
	healthDesc    *prometheus.Desc
	mutex         sync.Mutex
	cachedAt      time.Time
	cachedItems   []checkRow
	labelKeys     []string
	labelLabelKeys []string
}

type checkRow struct {
	ID        uuid.UUID                `gorm:"column:id"`
	AgentID   uuid.UUID                `gorm:"column:agent_id"`
	CanaryID  uuid.UUID                `gorm:"column:canary_id"`
	Name      string                   `gorm:"column:name"`
	Type      string                   `gorm:"column:type"`
	Namespace string                   `gorm:"column:namespace"`
	Labels    types.JSONStringMap      `gorm:"column:labels"`
	Status    models.CheckHealthStatus `gorm:"column:status"`
}

const (
	checksCacheTTLProperty    = "metrics.checks.cache_ttl"
	checksLabelsProperty      = "metrics.checks.labels"
	defaultChecksCacheTTL     = 5 * time.Minute
)

var checkInfoBaseLabels = []string{"id", "agent_id", "canary_id", "name", "type", "namespace"}

func newChecksCollector(ctx context.Context, includeInfo, includeHealth bool) *checksCollector {
	collector := &checksCollector{
		ctx:           ctx,
		includeInfo:   includeInfo,
		includeHealth: includeHealth,
	}
	if includeHealth {
		collector.healthDesc = prometheus.NewDesc(
			getMetricName(ctx, "checks_health"),
			"Check health status (1=healthy, 0=unhealthy).",
			[]string{"id", "agent_id", "canary_id"},
			nil,
		)
	}
	return collector
}

func (c *checksCollector) Describe(ch chan<- *prometheus.Desc) {
	if c.includeInfo {
		if c.infoDesc == nil {
			if _, err := c.getCachedItems(); err != nil {
				c.ctx.Logger.Errorf("failed to load checks for descriptor: %v", err)
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

func (c *checksCollector) Collect(ch chan<- prometheus.Metric) {
	if !c.includeInfo && !c.includeHealth {
		return
	}

	items, err := c.getCachedItems()
	if err != nil {
		c.ctx.Logger.Errorf("failed to collect checks: %v", err)
		return
	}

	infoReady := c.includeInfo && c.infoDesc != nil
	if c.includeInfo && c.infoDesc == nil {
		c.ctx.Logger.Errorf("checks info metric disabled: label descriptor not available")
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
				checkHealthValue(item.Status),
				item.ID.String(),
				agentID,
				formatUUID(item.CanaryID),
			)
		}
	}
}

func (c *checksCollector) infoLabelValues(item checkRow, agentID string) []string {
	labels := make([]string, 0, len(checkInfoBaseLabels)+len(c.labelKeys))
	labels = append(labels, item.ID.String(), agentID, formatUUID(item.CanaryID), item.Name, item.Type, item.Namespace)
	for _, key := range c.labelKeys {
		labels = append(labels, item.Labels[key])
	}
	return labels
}

func (c *checksCollector) ensureInfoDescriptor() {
	if !c.includeInfo || c.infoDesc != nil {
		return
	}

	if err := c.loadLabelKeys(); err != nil {
		c.ctx.Logger.Errorf("failed to load check label keys: %v", err)
		return
	}

	labels := append([]string(nil), checkInfoBaseLabels...)
	labels = append(labels, c.labelLabelKeys...)
	c.infoDesc = prometheus.NewDesc(
		getMetricName(c.ctx, "checks_info"),
		"Check metadata.",
		labels,
		nil,
	)
}

func (c *checksCollector) loadLabelKeys() error {
	var allLabelKeys []string
	if err := c.ctx.DB().Raw(`
		SELECT DISTINCT jsonb_object_keys(labels) AS key 
		FROM checks 
		WHERE deleted_at IS NULL AND labels IS NOT NULL
		ORDER BY key
	`).Pluck("key", &allLabelKeys).Error; err != nil {
		return err
	}

	// Filter labels based on configured patterns
	labelPatterns := c.ctx.Properties().String(checksLabelsProperty, "")
	var patterns []string
	if labelPatterns != "" {
		for _, p := range strings.Split(labelPatterns, ",") {
			if p = strings.TrimSpace(p); p != "" {
				patterns = append(patterns, p)
			}
		}
	}

	labelKeys := make([]string, 0, len(allLabelKeys))
	for _, key := range allLabelKeys {
		if collections.MatchItems(key, patterns...) {
			labelKeys = append(labelKeys, key)
		}
	}

	usedLabels := make(map[string]struct{}, len(checkInfoBaseLabels)+len(labelKeys))
	for _, label := range checkInfoBaseLabels {
		usedLabels[label] = struct{}{}
	}

	sanitizedKeys := make([]string, 0, len(labelKeys))
	for _, key := range labelKeys {
		label := sanitizeTagLabel(key)
		if _, exists := usedLabels[label]; exists {
			label = "label_" + label
		}
		label = ensureUniqueLabel(label, usedLabels)
		sanitizedKeys = append(sanitizedKeys, label)
	}

	c.labelKeys = labelKeys
	c.labelLabelKeys = sanitizedKeys
	return nil
}

func (c *checksCollector) getCachedItems() ([]checkRow, error) {
	cacheTTL := c.ctx.Properties().Duration(checksCacheTTLProperty, defaultChecksCacheTTL)
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

	items, err := c.fetchChecks()
	if err != nil {
		if hasCache {
			c.ctx.Logger.Errorf("failed to refresh checks cache: %v", err)
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

func (c *checksCollector) fetchChecks() ([]checkRow, error) {
	columns := []string{"id", "agent_id", "canary_id"}
	if c.includeInfo {
		columns = append(columns, "name", "type", "namespace", "labels")
	}
	if c.includeHealth {
		columns = append(columns, "status")
	}

	rows, err := c.ctx.DB().Model(&models.Check{}).
		Select(columns).
		Where("deleted_at IS NULL").
		Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]checkRow, 0)
	for rows.Next() {
		var item checkRow
		if err := c.ctx.DB().ScanRows(rows, &item); err != nil {
			c.ctx.Logger.Errorf("failed to scan check row: %v", err)
			continue
		}
		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return items, err
	}

	return items, nil
}

func checkHealthValue(status models.CheckHealthStatus) float64 {
	if status == models.CheckStatusHealthy {
		return 1
	}
	return 0
}
