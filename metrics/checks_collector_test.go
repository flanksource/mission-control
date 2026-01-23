package metrics

import (
	"strings"

	"github.com/flanksource/duty/models"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

var _ = ginkgo.Describe("ChecksCollector", func() {
	ginkgo.It("should emit correct info and health metrics", func() {
		collector := newChecksCollector(DefaultContext, true, true)

		// Collect metrics
		ch := make(chan prometheus.Metric, 1000)
		go func() {
			collector.Collect(ch)
			close(ch)
		}()

		var infoMetrics, healthMetrics []prometheus.Metric
		for metric := range ch {
			desc := metric.Desc().String()
			if strings.Contains(desc, "checks_info") {
				infoMetrics = append(infoMetrics, metric)
			} else if strings.Contains(desc, "checks_health") {
				healthMetrics = append(healthMetrics, metric)
			}
		}

		// Verify we got metrics for the dummy checks
		var checkCount int64
		err := DefaultContext.DB().Model(&models.Check{}).Where("deleted_at IS NULL").Count(&checkCount).Error
		Expect(err).ToNot(HaveOccurred())
		Expect(checkCount).To(BeNumerically(">", 0), "expected dummy checks to exist")

		Expect(infoMetrics).To(HaveLen(int(checkCount)), "expected one info metric per check")
		Expect(healthMetrics).To(HaveLen(int(checkCount)), "expected one health metric per check")

		// Verify health values are correct (1=healthy, 0=unhealthy)
		healthValues := make(map[float64]int)
		for _, metric := range healthMetrics {
			var m dto.Metric
			Expect(metric.Write(&m)).To(Succeed())
			healthValues[m.GetGauge().GetValue()]++
		}

		// From dummy data we know checks are healthy
		Expect(healthValues).To(HaveKey(float64(1)), "expected some healthy checks")
		Expect(healthValues[1]).To(BeNumerically(">", 0), "expected healthy count > 0")

		// Verify info metric has the expected labels
		var sampleMetric dto.Metric
		Expect(infoMetrics[0].Write(&sampleMetric)).To(Succeed())
		labelNames := make([]string, 0)
		for _, label := range sampleMetric.GetLabel() {
			labelNames = append(labelNames, label.GetName())
		}
		Expect(labelNames).To(ContainElements("id", "agent_id", "canary_id", "name", "type", "namespace"))
	})
})
