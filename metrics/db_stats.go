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
		if metricEnabled(ctx, "config_items_info") {
			prometheus.MustRegister(newConfigItemsInfoCollector(ctx))
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
	var checksCount int64
	if err := c.ctx.DB().Model(&models.Check{}).Where("deleted_at IS NULL").Count(&checksCount).Error; err != nil {
		c.ctx.Logger.Errorf("failed to collect checks count: %v", err)
	} else {
		ch <- prometheus.MustNewConstMetric(c.checksDesc, prometheus.GaugeValue, float64(checksCount))
	}

	var configItemsCount int64
	if err := c.ctx.DB().Model(&models.ConfigItem{}).Where("deleted_at IS NULL").Count(&configItemsCount).Error; err != nil {
		c.ctx.Logger.Errorf("failed to collect config items count: %v", err)
	} else {
		ch <- prometheus.MustNewConstMetric(c.configItemsDesc, prometheus.GaugeValue, float64(configItemsCount))
	}

	var dbSize int64
	if err := c.ctx.DB().Raw("SELECT pg_database_size(current_database())").Scan(&dbSize).Error; err != nil {
		c.ctx.Logger.Errorf("failed to collect database size: %v", err)
	} else {
		ch <- prometheus.MustNewConstMetric(c.dbSizeDesc, prometheus.GaugeValue, float64(dbSize))
	}

	var lastLoginSeconds float64
	if err := c.ctx.DB().Raw("SELECT COALESCE(EXTRACT(EPOCH FROM MAX(last_login)), 0) FROM users").Scan(&lastLoginSeconds).Error; err != nil {
		c.ctx.Logger.Errorf("failed to collect last login timestamp: %v", err)
	} else {
		ch <- prometheus.MustNewConstMetric(c.lastLoginDesc, prometheus.GaugeValue, lastLoginSeconds)
	}

	var loggedInUsers int64
	if err := c.ctx.DB().Raw("SELECT COUNT(*) FROM users WHERE last_login IS NOT NULL").Scan(&loggedInUsers).Error; err != nil {
		c.ctx.Logger.Errorf("failed to collect logged in users count: %v", err)
	} else {
		ch <- prometheus.MustNewConstMetric(c.loggedInUsersDesc, prometheus.GaugeValue, float64(loggedInUsers))
	}
}
