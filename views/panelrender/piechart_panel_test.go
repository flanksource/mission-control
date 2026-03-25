package panelrender_test

import (
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/dataquery"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/views"
)

var _ = ginkgo.Describe("PieChartPanel", func() {
	ginkgo.It("computes percentages from totals and renders all segments", func() {
		// Total = 100 so percentages equal the raw values — easy to reason about.
		result := &api.ViewResult{
			Name:  "piechart-panel-test",
			Title: "PieChart Panel Test",
			Panels: []api.PanelResult{
				{
					PanelMeta: api.PanelMeta{
						Name: "Health Distribution",
						Type: api.PanelTypePiechart,
					},
					Rows: []dataquery.QueryResultRow{
						{"label": "Healthy", "value": 75},
						{"label": "Degraded", "value": 20},
						{"label": "Failed", "value": 5},
					},
				},
			},
		}

		html, err := views.RenderFacetHTML(context.New(), result, nil)
		Expect(err).ToNot(HaveOccurred())

		page := string(html)

		Expect(page).ToNot(ContainSubstring("Unsupported panel type"))

		// All segment labels must render.
		Expect(page).To(ContainSubstring("Healthy"))
		Expect(page).To(ContainSubstring("Degraded"))
		Expect(page).To(ContainSubstring("Failed"))

		// The bar widths are set from pct = (value/total)*100.
		// With total=100 these equal the raw values, but they can only appear in
		// the width style if the division formula ran.
		// React SSR renders adjacent JSX expressions with <!-- --> separators, so
		// we assert on the style attribute (a plain string) rather than text content.
		Expect(page).To(ContainSubstring("width:75%"),
			"75/100*100 should produce a bar width of 75%")
		Expect(page).To(ContainSubstring("width:20%"),
			"20/100*100 should produce a bar width of 20%")
		Expect(page).To(ContainSubstring("width:5%"),
			"5/100*100 should produce a bar width of 5%")
	})
})
