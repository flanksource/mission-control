package metrics

import (
	"sync"
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
)

type agentsCollector struct {
	ctx           context.Context
	includeInfo   bool
	includeStatus bool
	infoDesc      *prometheus.Desc
	statusDesc    *prometheus.Desc
	mutex         sync.Mutex
	cachedAt      time.Time
	cachedItems   []agentRow
}

type agentRow struct {
	ID       uuid.UUID  `gorm:"column:id"`
	Name     string     `gorm:"column:name"`
	LastSeen *time.Time `gorm:"column:last_seen"`
}

const (
	agentsCacheTTLProperty = "metrics.agents.cache_ttl"
	defaultAgentsCacheTTL  = 5 * time.Minute
	agentOnlineThreshold   = 10 * time.Minute
)

var agentInfoBaseLabels = []string{"id", "name"}

func newAgentsCollector(ctx context.Context, includeInfo, includeStatus bool) *agentsCollector {
	collector := &agentsCollector{
		ctx:           ctx,
		includeInfo:   includeInfo,
		includeStatus: includeStatus,
	}
	if includeStatus {
		collector.statusDesc = prometheus.NewDesc(
			prometheus.BuildFQName("mission_control", "", "agent_status"),
			"Agent status (1=online, 0=offline).",
			[]string{"id"},
			nil,
		)
	}
	return collector
}

func (c *agentsCollector) Describe(ch chan<- *prometheus.Desc) {
	if c.includeInfo {
		if c.infoDesc == nil {
			if _, err := c.getCachedItems(); err != nil {
				c.ctx.Logger.Errorf("failed to load agents for descriptor: %v", err)
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

func (c *agentsCollector) Collect(ch chan<- prometheus.Metric) {
	if !c.includeInfo && !c.includeStatus {
		return
	}

	items, err := c.getCachedItems()
	if err != nil {
		c.ctx.Logger.Errorf("failed to collect agents: %v", err)
		return
	}

	infoReady := c.includeInfo && c.infoDesc != nil
	if c.includeInfo && c.infoDesc == nil {
		c.ctx.Logger.Errorf("agents info metric disabled: label descriptor not available")
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
			ch <- prometheus.MustNewConstMetric(
				c.statusDesc,
				prometheus.GaugeValue,
				agentStatusValue(item.LastSeen),
				item.ID.String(),
			)
		}
	}
}

func (c *agentsCollector) infoLabelValues(item agentRow) []string {
	return []string{item.ID.String(), item.Name}
}

func (c *agentsCollector) ensureInfoDescriptor() {
	if !c.includeInfo || c.infoDesc != nil {
		return
	}

	c.infoDesc = prometheus.NewDesc(
		prometheus.BuildFQName("mission_control", "", "agent_info"),
		"Agent metadata.",
		agentInfoBaseLabels,
		nil,
	)
}

func (c *agentsCollector) getCachedItems() ([]agentRow, error) {
	cacheTTL := c.ctx.Properties().Duration(agentsCacheTTLProperty, defaultAgentsCacheTTL)
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

	items, err := c.fetchAgents()
	if err != nil {
		if hasCache {
			c.ctx.Logger.Errorf("failed to refresh agents cache: %v", err)
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

func (c *agentsCollector) fetchAgents() ([]agentRow, error) {
	columns := []string{"id"}
	if c.includeInfo {
		columns = append(columns, "name")
	}
	if c.includeStatus {
		columns = append(columns, "last_seen")
	}

	rows, err := c.ctx.DB().Model(&models.Agent{}).
		Select(columns).
		Where("deleted_at IS NULL").
		Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]agentRow, 0)
	for rows.Next() {
		var item agentRow
		if err := c.ctx.DB().ScanRows(rows, &item); err != nil {
			c.ctx.Logger.Errorf("failed to scan agent row: %v", err)
			continue
		}
		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return items, err
	}

	return items, nil
}

func agentStatusValue(lastSeen *time.Time) float64 {
	if lastSeen == nil {
		return 0
	}
	if time.Since(*lastSeen) < agentOnlineThreshold {
		return 1
	}
	return 0
}
