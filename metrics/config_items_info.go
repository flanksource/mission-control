package metrics

import (
	"fmt"
	"sort"
	"strings"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/samber/lo"
)

type configItemsInfoCollector struct {
	ctx      context.Context
	infoDesc *prometheus.Desc
}

type configItemInfoRow struct {
	ID   uuid.UUID           `gorm:"column:id"`
	Name *string             `gorm:"column:name"`
	Tags types.JSONStringMap `gorm:"column:tags"`
}

func newConfigItemsInfoCollector(ctx context.Context) *configItemsInfoCollector {
	return &configItemsInfoCollector{
		ctx:      ctx,
		infoDesc: prometheus.NewDesc(prometheus.BuildFQName("mission_control", "", "config_items_info"), "Config item metadata.", []string{"id", "name", "namespace", "tags"}, nil),
	}
}

func (c *configItemsInfoCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.infoDesc
}

func (c *configItemsInfoCollector) Collect(ch chan<- prometheus.Metric) {
	rows, err := c.ctx.DB().Model(&models.ConfigItem{}).
		Select("id", "name", "tags").
		Where("deleted_at IS NULL").
		Rows()
	if err != nil {
		c.ctx.Logger.Errorf("failed to collect config items info: %v", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var item configItemInfoRow
		if err := c.ctx.DB().ScanRows(rows, &item); err != nil {
			c.ctx.Logger.Errorf("failed to scan config item row: %v", err)
			continue
		}

		namespace := item.Tags["namespace"]
		ch <- prometheus.MustNewConstMetric(
			c.infoDesc,
			prometheus.GaugeValue,
			1,
			item.ID.String(),
			lo.FromPtr(item.Name),
			namespace,
			formatConfigItemTags(item.Tags),
		)
	}

	if err := rows.Err(); err != nil {
		c.ctx.Logger.Errorf("failed to iterate config items info: %v", err)
	}
}

func formatConfigItemTags(tags types.JSONStringMap) string {
	if len(tags) == 0 {
		return ""
	}

	keys := make([]string, 0, len(tags))
	for key := range tags {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, fmt.Sprintf("%s=%s", key, tags[key]))
	}

	return strings.Join(pairs, ",")
}
