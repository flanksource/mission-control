package metrics

import (
	"strings"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

var _ = ginkgo.Describe("ConfigItemsCollector", func() {
	ginkgo.It("should emit correct info and health metrics", func() {
		collector := newConfigItemsCollector(DefaultContext, true, true)

		// Collect metrics
		ch := make(chan prometheus.Metric, 1000)
		go func() {
			collector.Collect(ch)
			close(ch)
		}()

		var infoMetrics, healthMetrics []prometheus.Metric
		for metric := range ch {
			desc := metric.Desc().String()
			if strings.Contains(desc, "config_items_info") {
				infoMetrics = append(infoMetrics, metric)
			} else if strings.Contains(desc, "config_items_health") {
				healthMetrics = append(healthMetrics, metric)
			}
		}

		// Verify we got metrics for the dummy config items
		var configCount int64
		err := DefaultContext.DB().Model(&models.ConfigItem{}).Where("deleted_at IS NULL").Count(&configCount).Error
		Expect(err).ToNot(HaveOccurred())
		Expect(configCount).To(BeNumerically(">", 0), "expected dummy config items to exist")

		Expect(infoMetrics).To(HaveLen(int(configCount)), "expected one info metric per config item")
		Expect(healthMetrics).To(HaveLen(int(configCount)), "expected one health metric per config item")

		// Verify health values are correct
		healthValues := make(map[float64]int)
		for _, metric := range healthMetrics {
			var m dto.Metric
			Expect(metric.Write(&m)).To(Succeed())
			healthValues[m.GetGauge().GetValue()]++
		}

		// Verify health mapping: 0=healthy, 1=warning, 2=unhealthy, 3=unknown
		// From dummy data we know EKSCluster has HealthUnknown, others have HealthHealthy
		Expect(healthValues).To(HaveKey(float64(0)), "expected some healthy config items")
		Expect(healthValues[0]).To(BeNumerically(">", 0), "expected healthy count > 0")

		// Verify info metric has the expected labels including type
		var sampleMetric dto.Metric
		Expect(infoMetrics[0].Write(&sampleMetric)).To(Succeed())
		labelNames := make([]string, 0)
		for _, label := range sampleMetric.GetLabel() {
			labelNames = append(labelNames, label.GetName())
		}
		Expect(labelNames).To(ContainElements("id", "agent_id", "name", "type", "namespace"))

		// Verify a known config item's health
		eksCluster := dummy.EKSCluster
		for _, metric := range healthMetrics {
			var m dto.Metric
			Expect(metric.Write(&m)).To(Succeed())
			for _, label := range m.GetLabel() {
				if label.GetName() == "id" && label.GetValue() == eksCluster.ID.String() {
					// EKSCluster has HealthUnknown which maps to 3
					Expect(m.GetGauge().GetValue()).To(Equal(float64(3)))
				}
			}
		}
	})
})
