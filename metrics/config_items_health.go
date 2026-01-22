package metrics

import (
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
)

type configItemsHealthCollector struct {
	ctx        context.Context
	healthDesc *prometheus.Desc
}

type configItemHealthRow struct {
	ID     uuid.UUID      `gorm:"column:id"`
	Health *models.Health `gorm:"column:health"`
}

func newConfigItemsHealthCollector(ctx context.Context) *configItemsHealthCollector {
	return &configItemsHealthCollector{
		ctx: ctx,
		healthDesc: prometheus.NewDesc(
			prometheus.BuildFQName("mission_control", "", "config_items_health"),
			"Config item health status (0=healthy, 1=warning, 2=error).",
			[]string{"config_id"},
			nil,
		),
	}
}

func (c *configItemsHealthCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.healthDesc
}

func (c *configItemsHealthCollector) Collect(ch chan<- prometheus.Metric) {
	rows, err := c.ctx.DB().Model(&models.ConfigItem{}).
		Select("id", "health").
		Where("deleted_at IS NULL").
		Rows()
	if err != nil {
		c.ctx.Logger.Errorf("failed to collect config items health: %v", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var item configItemHealthRow
		if err := c.ctx.DB().ScanRows(rows, &item); err != nil {
			c.ctx.Logger.Errorf("failed to scan config item health row: %v", err)
			continue
		}

		ch <- prometheus.MustNewConstMetric(
			c.healthDesc,
			prometheus.GaugeValue,
			configItemHealthValue(item.Health),
			item.ID.String(),
		)
	}

	if err := rows.Err(); err != nil {
		c.ctx.Logger.Errorf("failed to iterate config items health: %v", err)
	}
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
