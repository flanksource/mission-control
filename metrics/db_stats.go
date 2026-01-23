package metrics

import (
	"sync"

	"github.com/flanksource/duty/context"
	"github.com/prometheus/client_golang/prometheus"
)

var registerDBStatsOnce sync.Once

func RegisterDBStats(ctx context.Context) {
	registerDBStatsOnce.Do(func() {
		prometheus.MustRegister(newDBStatsCollector(ctx))
		enableConfigInfo := metricEnabled(ctx, "config_items_info")
		enableConfigHealth := metricEnabled(ctx, "config_items_health")
		if enableConfigInfo || enableConfigHealth {
			prometheus.MustRegister(newConfigItemsCollector(ctx, enableConfigInfo, enableConfigHealth))
		}

		enableChecksInfo := metricEnabled(ctx, "checks_info")
		enableChecksHealth := metricEnabled(ctx, "checks_health")
		if enableChecksInfo || enableChecksHealth {
			prometheus.MustRegister(newChecksCollector(ctx, enableChecksInfo, enableChecksHealth))
		}

		enableAgentInfo := metricEnabled(ctx, "agent_info")
		enableAgentStatus := metricEnabled(ctx, "agent_status")
		if enableAgentInfo || enableAgentStatus {
			prometheus.MustRegister(newAgentsCollector(ctx, enableAgentInfo, enableAgentStatus))
		}

		if metricEnabled(ctx, "scrapers_info") {
			prometheus.MustRegister(newScrapersCollector(ctx))
		}
	})
}

type dbStatsCollector struct {
	ctx               context.Context
	dbSizeDesc        *prometheus.Desc
	lastLoginDesc     *prometheus.Desc
	loggedInUsersDesc *prometheus.Desc
}

func newDBStatsCollector(ctx context.Context) *dbStatsCollector {
	return &dbStatsCollector{
		ctx:               ctx,
		dbSizeDesc:        prometheus.NewDesc(prometheus.BuildFQName("mission_control", "", "db_size_bytes"), "Size of the database in bytes.", nil, nil),
		lastLoginDesc:     prometheus.NewDesc(prometheus.BuildFQName("mission_control", "", "last_login_timestamp_seconds"), "Latest user login timestamp in seconds since epoch.", nil, nil),
		loggedInUsersDesc: prometheus.NewDesc(prometheus.BuildFQName("mission_control", "", "active_sessions"), "Active unexpired sessions/tokens.", nil, nil),
	}
}

func (c *dbStatsCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.dbSizeDesc
	ch <- c.lastLoginDesc
	ch <- c.loggedInUsersDesc
}

func (c *dbStatsCollector) Collect(ch chan<- prometheus.Metric) {
	collectGauge := func(desc *prometheus.Desc, errMsg string, fn func() (float64, error)) {
		value, err := fn()
		if err != nil {
			c.ctx.Logger.Errorf("%s: %v", errMsg, err)
			return
		}
		ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, value)
	}

	collectGauge(c.dbSizeDesc, "failed to collect database size", func() (float64, error) {
		var dbSize int64
		err := c.ctx.DB().Raw("SELECT pg_database_size(current_database())").Scan(&dbSize).Error
		return float64(dbSize), err
	})

	collectGauge(c.lastLoginDesc, "failed to collect last login timestamp", func() (float64, error) {
		var lastLoginSeconds float64
		err := c.ctx.DB().Raw("SELECT COALESCE(EXTRACT(EPOCH FROM MAX(last_login)), 0) FROM users").Scan(&lastLoginSeconds).Error
		return lastLoginSeconds, err
	})

	collectGauge(c.loggedInUsersDesc, "failed to collect active sessions count", func() (float64, error) {
		var activeSessions int64
		err := c.ctx.DB().Raw("SELECT COUNT(*) FROM access_tokens WHERE expires_at IS NULL OR expires_at > NOW()").Scan(&activeSessions).Error
		return float64(activeSessions), err
	})
}
