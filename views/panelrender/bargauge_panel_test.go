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

var _ = ginkgo.Describe("BarGaugePanel", func() {
	ginkgo.It("renders labeled bars with computed percentages and formatted values", func() {
		result := &api.ViewResult{
			Name:  "bargauge-panel-test",
			Title: "BarGauge Panel Test",
			Panels: []api.PanelResult{
				{
					PanelMeta: api.PanelMeta{
						Name: "Memory by Namespace",
						Type: api.PanelTypeBargauge,
						Bargauge: &api.PanelBargaugeConfig{
							GaugeConfig: pkgView.GaugeConfig{
								Min: "0",
								Max: "16384",
							},
							Unit: "MiB",
						},
					},
					Rows: []dataquery.QueryResultRow{
						// 4096 / 16384 = exactly 25%
						{"name": "kube-system", "value": 4096},
						// 8192 / 16384 = exactly 50%
						{"name": "monitoring", "value": 8192},
					},
				},
			},
		}

		html, err := views.RenderFacetHTML(context.New(), result, nil)
		Expect(err).ToNot(HaveOccurred())

		page := string(html)

		Expect(page).ToNot(ContainSubstring("Unsupported panel type"))

		// Labels must appear.
		Expect(page).To(ContainSubstring("kube-system"))
		Expect(page).To(ContainSubstring("monitoring"))

		// formatDisplayValue(4096, "MiB", 0) → "4096 MiB"
		// Proves the unit-aware formatter ran.
		Expect(page).To(ContainSubstring("4096 MiB"))
		Expect(page).To(ContainSubstring("8192 MiB"))

		// Bar widths are computed from (value - min) / (max - min) * 100.
		// 4096 / 16384 * 100 = 25 → style="width: 25%"
		// 8192 / 16384 * 100 = 50 → style="width: 50%"
		// These can only appear if the percentage formula ran on the correct max.
		Expect(page).To(ContainSubstring("25%"),
			"4096/16384*100 must equal 25%")
		Expect(page).To(ContainSubstring("50%"),
			"8192/16384*100 must equal 50%")
	})
})
