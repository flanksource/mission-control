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
	})
}

type dbStatsCollector struct {
	ctx             context.Context
	checksDesc      *prometheus.Desc
	configItemsDesc *prometheus.Desc
	dbSizeDesc      *prometheus.Desc
}

func newDBStatsCollector(ctx context.Context) *dbStatsCollector {
	return &dbStatsCollector{
		ctx:             ctx,
		checksDesc:      prometheus.NewDesc(prometheus.BuildFQName("mission_control", "", "checks_total"), "Total number of checks.", nil, nil),
		configItemsDesc: prometheus.NewDesc(prometheus.BuildFQName("mission_control", "", "config_items_total"), "Total number of config items.", nil, nil),
		dbSizeDesc:      prometheus.NewDesc(prometheus.BuildFQName("mission_control", "", "db_size_bytes"), "Size of the database in bytes.", nil, nil),
	}
}

func (c *dbStatsCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.checksDesc
	ch <- c.configItemsDesc
	ch <- c.dbSizeDesc
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
}
