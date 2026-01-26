package metrics

import (
	"sync"
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
)

type canariesCollector struct {
	ctx           context.Context
	includeInfo   bool
	includeStatus bool
	infoDesc      *prometheus.Desc
	statusDesc    *prometheus.Desc
	mutex         sync.Mutex
	cachedAt      time.Time
	cachedItems   []canaryRow
}

type canaryRow struct {
	ID        uuid.UUID           `gorm:"column:id"`
	AgentID   uuid.UUID           `gorm:"column:agent_id"`
	Name      string              `gorm:"column:name"`
	Namespace string              `gorm:"column:namespace"`
	Labels    types.JSONStringMap `gorm:"column:labels"`
	Source    string              `gorm:"column:source"`
}

const (
	canariesCacheTTLProperty = "metrics.canaries.cache_ttl"
	defaultCanariesCacheTTL  = 5 * time.Minute
)

var canaryInfoBaseLabels = []string{"id", "agent_id", "name", "namespace", "source"}

func newCanariesCollector(ctx context.Context, includeInfo, includeStatus bool) *canariesCollector {
	collector := &canariesCollector{
		ctx:           ctx,
		includeInfo:   includeInfo,
		includeStatus: includeStatus,
	}
	if includeStatus {
		collector.statusDesc = prometheus.NewDesc(
			getMetricName(ctx, "canary_status"),
			"Canary status based on associated checks (1=healthy, 0=unhealthy).",
			[]string{"id"},
			nil,
		)
	}
	return collector
}

func (c *canariesCollector) Describe(ch chan<- *prometheus.Desc) {
	if c.includeInfo {
		if c.infoDesc == nil {
			if _, err := c.getCachedItems(); err != nil {
				c.ctx.Logger.Errorf("failed to load canaries for descriptor: %v", err)
			}
		}
		if c.infoDesc != nil {
			ch <- c.infoDesc
		}
	}
	if c.includeStatus {
		ch <- c.statusDesc
	}
}

func (c *canariesCollector) Collect(ch chan<- prometheus.Metric) {
	if !c.includeInfo && !c.includeStatus {
		return
	}

	items, err := c.getCachedItems()
	if err != nil {
		c.ctx.Logger.Errorf("failed to collect canaries: %v", err)
		return
	}

	infoReady := c.includeInfo && c.infoDesc != nil
	if c.includeInfo && c.infoDesc == nil {
		c.ctx.Logger.Errorf("canaries info metric disabled: label descriptor not available")
	}

	for _, item := range items {
		if infoReady {
			labels := c.infoLabelValues(item)
			ch <- prometheus.MustNewConstMetric(
				c.infoDesc,
				prometheus.GaugeValue,
				1,
				labels...,
			)
		}

		if c.includeStatus {
			status, err := c.getCanaryStatus(item.ID)
			if err != nil {
				c.ctx.Logger.Errorf("failed to get canary status for %s: %v", item.ID, err)
				continue
			}
			ch <- prometheus.MustNewConstMetric(
				c.statusDesc,
				prometheus.GaugeValue,
				status,
				formatUUID(item.ID),
			)
		}
	}
}

func (c *canariesCollector) infoLabelValues(item canaryRow) []string {
	return []string{formatUUID(item.ID), formatUUID(item.AgentID), item.Name, item.Namespace, item.Source}
}

func (c *canariesCollector) ensureInfoDescriptor() {
	if !c.includeInfo || c.infoDesc != nil {
		return
	}

	c.infoDesc = prometheus.NewDesc(
		getMetricName(c.ctx, "canary_info"),
		"Canary metadata.",
		canaryInfoBaseLabels,
		nil,
	)
}

func (c *canariesCollector) getCachedItems() ([]canaryRow, error) {
	cacheTTL := c.ctx.Properties().Duration(canariesCacheTTLProperty, defaultCanariesCacheTTL)
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

	items, err := c.fetchCanaries()
	if err != nil {
		if hasCache {
			c.ctx.Logger.Errorf("failed to refresh canaries cache: %v", err)
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

func (c *canariesCollector) fetchCanaries() ([]canaryRow, error) {
	columns := []string{"id", "agent_id", "name", "namespace", "source"}

	rows, err := c.ctx.DB().Model(&models.Canary{}).
		Select(columns).
		Where("deleted_at IS NULL").
		Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]canaryRow, 0)
	for rows.Next() {
		var item canaryRow
		if err := c.ctx.DB().ScanRows(rows, &item); err != nil {
			c.ctx.Logger.Errorf("failed to scan canary row: %v", err)
			continue
		}
		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return items, err
	}

	return items, nil
}

// getCanaryStatus returns 1 if all checks for the canary are healthy, 0 otherwise.
func (c *canariesCollector) getCanaryStatus(canaryID uuid.UUID) (float64, error) {
	var unhealthyCount int64
	err := c.ctx.DB().Model(&models.Check{}).
		Where("canary_id = ? AND deleted_at IS NULL AND status != ?", canaryID, models.CheckStatusHealthy).
		Count(&unhealthyCount).Error
	if err != nil {
		return 0, err
	}

	if unhealthyCount > 0 {
		return 0, nil
	}
	return 1, nil
}
