package metrics

import (
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
)

type notificationsCollector struct {
	ctx  context.Context
	desc *prometheus.Desc
}

type notificationStatusRow struct {
	NotificationID uuid.UUID `gorm:"column:notification_id"`
	Status         string    `gorm:"column:status"`
	AgentID        uuid.UUID `gorm:"column:agent_id"`
}

func newNotificationsCollector(ctx context.Context) *notificationsCollector {
	return &notificationsCollector{
		ctx: ctx,
		desc: prometheus.NewDesc(
			prometheus.BuildFQName("mission_control", "", "notifications"),
			"Notification status (-1=suppressed, 0=firing, 1=sent).",
			[]string{"notification_id", "agent_id"},
			nil,
		),
	}
}

func (c *notificationsCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.desc
}

func (c *notificationsCollector) Collect(ch chan<- prometheus.Metric) {
	rows, err := c.ctx.DB().Table("notification_send_history_summary AS nsh").
		Select(`DISTINCT ON (nsh.notification_id) nsh.notification_id,
			nsh.status,
			COALESCE(ci.agent_id, comp.agent_id, chk.agent_id, can.agent_id, '00000000-0000-0000-0000-000000000000') AS agent_id`).
		Joins("LEFT JOIN config_items ci ON nsh.resource_kind = 'config' AND ci.id = nsh.resource_id").
		Joins("LEFT JOIN components comp ON nsh.resource_kind = 'component' AND comp.id = nsh.resource_id").
		Joins("LEFT JOIN checks chk ON nsh.resource_kind = 'check' AND chk.id = nsh.resource_id").
		Joins("LEFT JOIN canaries can ON nsh.resource_kind = 'canary' AND can.id = nsh.resource_id").
		Order("nsh.notification_id, nsh.created_at DESC").
		Rows()
	if err != nil {
		c.ctx.Logger.Errorf("failed to collect notifications: %v", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var row notificationStatusRow
		if err := c.ctx.DB().ScanRows(rows, &row); err != nil {
			c.ctx.Logger.Errorf("failed to scan notification row: %v", err)
			continue
		}

		ch <- prometheus.MustNewConstMetric(
			c.desc,
			prometheus.GaugeValue,
			notificationStatusValue(row.Status),
			row.NotificationID.String(),
			formatUUID(row.AgentID),
		)
	}

	if err := rows.Err(); err != nil {
		c.ctx.Logger.Errorf("failed to iterate notifications: %v", err)
	}
}

func notificationStatusValue(status string) float64 {
	switch status {
	case models.NotificationStatusSent:
		return 1
	case models.NotificationStatusSilenced,
		models.NotificationStatusInhibited,
		models.NotificationStatusRepeatInterval,
		models.NotificationStatusSkipped,
		notificationStatusGrouped:
		return -1
	default:
		return 0
	}
}

const notificationStatusGrouped = "grouped"
