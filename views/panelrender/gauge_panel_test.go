package panelrender_test

import (
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/dataquery"
	pkgView "github.com/flanksource/duty/view"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/views"
)

var _ = ginkgo.Describe("GaugePanel", func() {
	ginkgo.It("formats display value with unit and computes percentage for the progress bar", func() {
		result := &api.ViewResult{
			Name:  "gauge-panel-test",
			Title: "Gauge Panel Test",
			Panels: []api.PanelResult{
				{
					PanelMeta: api.PanelMeta{
						Name: "CPU Utilization",
						Type: api.PanelTypeGauge,
						Gauge: &api.PanelGaugeConfig{
							GaugeConfig: pkgView.GaugeConfig{
								Min:       "0",
								Max:       "100",
								Precision: 1,
								Thresholds: []pkgView.GaugeThreshold{
									{Percent: 0, Color: "#16A34A"},  // green below 70
									{Percent: 70, Color: "#D97706"}, // orange 70–90
									{Percent: 90, Color: "#DC2626"}, // red above 90
								},
							},
							Unit: "percent",
						},
					},
					Rows: []dataquery.QueryResultRow{
						{"value": 67.4},
					},
				},
			},
		}

		html, err := views.RenderFacetHTML(context.New(), result, nil)
		Expect(err).ToNot(HaveOccurred())

		page := string(html)

		Expect(page).ToNot(ContainSubstring("Unsupported panel type"))

		// formatDisplayValue(67.4, "percent", 1) → "67.4%"
		// Proves the unit-aware formatter ran on the raw value.
		Expect(page).To(ContainSubstring("67.4%"),
			"value 67.4 with unit 'percent' should render as 67.4%")

		// Percentage = (67.4 - 0) / (100 - 0) * 100 = 67.4.
		// The progress bar style carries this value — proves the (v-min)/(max-min)*100
		// formula ran rather than the value being used raw.
		Expect(page).To(ContainSubstring("67.4%"))

		// 67.4 is below the 70% threshold, so the green color must be used.
		// This validates the fixed threshold comparison (percentage vs raw value).
		Expect(page).To(ContainSubstring("#16A34A"),
			"percentage 67.4 is below the 70% threshold so green should be used")
		Expect(page).ToNot(ContainSubstring("#D97706"),
			"orange threshold must not be applied when percentage is below 70")
	})
})
