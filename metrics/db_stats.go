package metrics

import (
	"sync"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
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
		if metricEnabled(ctx, "scrapers_info") {
			prometheus.MustRegister(newScrapersCollector(ctx))
		}
	})
}

type dbStatsCollector struct {
	ctx               context.Context
	checksDesc        *prometheus.Desc
	configItemsDesc   *prometheus.Desc
	dbSizeDesc        *prometheus.Desc
	lastLoginDesc     *prometheus.Desc
	loggedInUsersDesc *prometheus.Desc
}

func newDBStatsCollector(ctx context.Context) *dbStatsCollector {
	return &dbStatsCollector{
		ctx:               ctx,
		checksDesc:        prometheus.NewDesc(prometheus.BuildFQName("mission_control", "", "checks_total"), "Total number of checks.", nil, nil),
		configItemsDesc:   prometheus.NewDesc(prometheus.BuildFQName("mission_control", "", "config_items_total"), "Total number of config items.", nil, nil),
		dbSizeDesc:        prometheus.NewDesc(prometheus.BuildFQName("mission_control", "", "db_size_bytes"), "Size of the database in bytes.", nil, nil),
		lastLoginDesc:     prometheus.NewDesc(prometheus.BuildFQName("mission_control", "", "last_login_timestamp_seconds"), "Latest user login timestamp in seconds since epoch.", nil, nil),
		loggedInUsersDesc: prometheus.NewDesc(prometheus.BuildFQName("mission_control", "", "logged_in_users_total"), "Total number of distinct users that have logged in.", nil, nil),
	}
}

func (c *dbStatsCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.checksDesc
	ch <- c.configItemsDesc
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

	collectGauge(c.checksDesc, "failed to collect checks count", func() (float64, error) {
		var checksCount int64
		err := c.ctx.DB().Model(&models.Check{}).Where("deleted_at IS NULL").Count(&checksCount).Error
		return float64(checksCount), err
	})

	collectGauge(c.configItemsDesc, "failed to collect config items count", func() (float64, error) {
		var configItemsCount int64
		err := c.ctx.DB().Model(&models.ConfigItem{}).Where("deleted_at IS NULL").Count(&configItemsCount).Error
		return float64(configItemsCount), err
	})

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

	collectGauge(c.loggedInUsersDesc, "failed to collect logged in users count", func() (float64, error) {
		var loggedInUsers int64
		err := c.ctx.DB().Raw("SELECT COUNT(*) FROM users WHERE last_login IS NOT NULL").Scan(&loggedInUsers).Error
		return float64(loggedInUsers), err
	})
}
