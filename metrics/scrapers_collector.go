package metrics

import (
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/samber/lo"
)

type scrapersCollector struct {
	ctx  context.Context
	desc *prometheus.Desc
}

type scraperInfoRow struct {
	ID        uuid.UUID `gorm:"column:id"`
	AgentID   uuid.UUID `gorm:"column:agent_id"`
	Name      string    `gorm:"column:name"`
	Namespace *string   `gorm:"column:namespace"`
	Source    string    `gorm:"column:source"`
}

func newScrapersCollector(ctx context.Context) *scrapersCollector {
	return &scrapersCollector{
		ctx: ctx,
		desc: prometheus.NewDesc(
			getMetricName(ctx, "scrapers_info"),
			"Config scrapers metadata.",
			[]string{"id", "agent_id", "name", "namespace", "source"},
			nil,
		),
	}
}

func (c *scrapersCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.desc
}

func (c *scrapersCollector) Collect(ch chan<- prometheus.Metric) {
	rows, err := c.ctx.DB().Model(&models.ConfigScraper{}).
		Select("id", "agent_id", "name", "namespace", "source").
		Where("deleted_at IS NULL").
		Rows()
	if err != nil {
		c.ctx.Logger.Errorf("failed to collect scrapers: %v", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var row scraperInfoRow
		if err := c.ctx.DB().ScanRows(rows, &row); err != nil {
			c.ctx.Logger.Errorf("failed to scan scraper row: %v", err)
			continue
		}

		ch <- prometheus.MustNewConstMetric(
			c.desc,
			prometheus.GaugeValue,
			1,
			row.ID.String(),
			formatUUID(row.AgentID),
			row.Name,
			lo.FromPtr(row.Namespace),
			row.Source,
		)
	}

	if err := rows.Err(); err != nil {
		c.ctx.Logger.Errorf("failed to iterate scrapers: %v", err)
	}
}
