package e2e

import (
	"fmt"
	"net/http"
	"time"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

func fetchAndParseMetrics(url string) (map[string]*dto.MetricFamily, error) {
	client := &http.Client{
		Timeout: 5 * time.Second,
	}
	resp, err := client.Get(url + "/metrics")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	parser := expfmt.NewTextParser(model.LegacyValidation)
	return parser.TextToMetricFamilies(resp.Body)
}

func getMetricLabels(m *dto.Metric) map[string]string {
	labels := make(map[string]string)
	for _, l := range m.GetLabel() {
		labels[l.GetName()] = l.GetValue()
	}
	return labels
}

var _ = ginkgo.Describe("Metrics", func() {
	ginkgo.It("should expose config_items metrics with correct labels and values", func() {
		families, err := fetchAndParseMetrics(server.URL)
		Expect(err).ToNot(HaveOccurred())

		// Get expected config count
		var configCount int64
		err = DefaultContext.DB().Model(&models.ConfigItem{}).Where("deleted_at IS NULL").Count(&configCount).Error
		Expect(err).ToNot(HaveOccurred())
		Expect(configCount).To(BeNumerically(">", 0))

		// Verify config_items_info metrics
		infoFamily, ok := families["mission_control_config_items_info"]
		Expect(ok).To(BeTrue(), "mission_control_config_items_info metric should exist")
		Expect(infoFamily.GetMetric()).To(HaveLen(int(configCount)))

		for _, m := range infoFamily.GetMetric() {
			labels := getMetricLabels(m)
			Expect(labels).To(HaveKey("id"))
			Expect(labels).To(HaveKey("agent_id"))
			Expect(labels).To(HaveKey("name"))
			Expect(labels).To(HaveKey("type"))
			Expect(labels).To(HaveKey("namespace"))
			Expect(m.GetGauge().GetValue()).To(Equal(float64(1)))
		}

		// Verify config_items_health metrics
		healthFamily, ok := families["mission_control_config_items_health"]
		Expect(ok).To(BeTrue(), "mission_control_config_items_health metric should exist")
		Expect(healthFamily.GetMetric()).To(HaveLen(int(configCount)))

		var eksHealthValue float64
		for _, m := range healthFamily.GetMetric() {
			labels := getMetricLabels(m)
			Expect(labels).To(HaveKey("id"))
			Expect(labels).To(HaveKey("agent_id"))

			value := m.GetGauge().GetValue()
			Expect(value).To(BeNumerically(">=", 0))
			Expect(value).To(BeNumerically("<=", 3))

			if labels["id"] == dummy.EKSCluster.ID.String() {
				eksHealthValue = value
			}
		}

		// EKSCluster has HealthUnknown = 3
		Expect(eksHealthValue).To(Equal(float64(3)))
	})

	ginkgo.It("should expose checks metrics with correct labels and values", func() {
		families, err := fetchAndParseMetrics(server.URL)
		Expect(err).ToNot(HaveOccurred())

		// Get expected check count
		var checkCount int64
		err = DefaultContext.DB().Model(&models.Check{}).Where("deleted_at IS NULL").Count(&checkCount).Error
		Expect(err).ToNot(HaveOccurred())
		Expect(checkCount).To(BeNumerically(">", 0))

		// Verify checks_info metrics
		infoFamily, ok := families["mission_control_checks_info"]
		Expect(ok).To(BeTrue(), "mission_control_checks_info metric should exist")
		Expect(infoFamily.GetMetric()).To(HaveLen(int(checkCount)))

		for _, m := range infoFamily.GetMetric() {
			labels := getMetricLabels(m)
			Expect(labels).To(HaveKey("id"))
			Expect(labels).To(HaveKey("agent_id"))
			Expect(labels).To(HaveKey("canary_id"))
			Expect(labels).To(HaveKey("name"))
			Expect(labels).To(HaveKey("type"))
			Expect(labels).To(HaveKey("namespace"))
			Expect(m.GetGauge().GetValue()).To(Equal(float64(1)))
		}

		// Verify checks_health metrics
		healthFamily, ok := families["mission_control_checks_health"]
		Expect(ok).To(BeTrue(), "mission_control_checks_health metric should exist")
		Expect(healthFamily.GetMetric()).To(HaveLen(int(checkCount)))

		healthyCount := 0
		for _, m := range healthFamily.GetMetric() {
			labels := getMetricLabels(m)
			Expect(labels).To(HaveKey("id"))
			Expect(labels).To(HaveKey("agent_id"))

			value := m.GetGauge().GetValue()
			Expect(value).To(BeElementOf(float64(0), float64(1)))

			if value == 1 {
				healthyCount++
			}
		}

		// Dummy checks are healthy
		Expect(healthyCount).To(BeNumerically(">", 0))
	})

	ginkgo.It("should expose db stats metrics with correct values", func() {
		families, err := fetchAndParseMetrics(server.URL)
		Expect(err).ToNot(HaveOccurred())

		// Verify db_size_bytes
		dbSizeFamily, ok := families["mission_control_db_size_bytes"]
		Expect(ok).To(BeTrue(), "mission_control_db_size_bytes metric should exist")
		Expect(dbSizeFamily.GetMetric()).To(HaveLen(1))
		Expect(dbSizeFamily.GetMetric()[0].GetGauge().GetValue()).To(BeNumerically(">", 0))

		// Verify active_sessions
		sessionsFamily, ok := families["mission_control_active_sessions"]
		Expect(ok).To(BeTrue(), "mission_control_active_sessions metric should exist")
		Expect(sessionsFamily.GetMetric()).To(HaveLen(1))
		Expect(sessionsFamily.GetMetric()[0].GetGauge().GetValue()).To(BeNumerically(">=", 0))

		// Note: last_login_timestamp_seconds not tested as it requires the users view which depends on Kratos
	})
})
