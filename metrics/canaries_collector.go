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

type canaryStatusRow struct {
	CanaryID       uuid.UUID `gorm:"column:canary_id"`
	UnhealthyCount int64     `gorm:"column:unhealthy"`
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

	var statuses map[uuid.UUID]float64
	statusReady := false
	if c.includeStatus {
		statusMap, err := c.getCanaryStatusMap()
		if err != nil {
			c.ctx.Logger.Errorf("failed to load canary statuses: %v", err)
		} else {
			statuses = statusMap
			statusReady = true
		}
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

		if statusReady {
			status := float64(1)
			if statusValue, ok := statuses[item.ID]; ok {
				status = statusValue
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

// getCanaryStatusMap returns 1 if all checks for the canary are healthy, 0 otherwise.
func (c *canariesCollector) getCanaryStatusMap() (map[uuid.UUID]float64, error) {
	var rows []canaryStatusRow
	err := c.ctx.DB().Model(&models.Check{}).
		Select("canary_id, COUNT(*) FILTER (WHERE status != ?) AS unhealthy", models.CheckStatusHealthy).
		Where("deleted_at IS NULL AND canary_id IS NOT NULL").
		Group("canary_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	statuses := make(map[uuid.UUID]float64, len(rows))
	for _, row := range rows {
		if row.UnhealthyCount > 0 {
			statuses[row.CanaryID] = 0
			continue
		}
		statuses[row.CanaryID] = 1
	}

	return statuses, nil
}
