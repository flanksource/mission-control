package panelrender_test

import (
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/dataquery"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/views"
)

var _ = ginkgo.Describe("NumberPanel", func() {
	ginkgo.It("formats all rows with unit and supports count fallback", func() {
		result := &api.ViewResult{
			Name:  "number-panel-test",
			Title: "Number Panel Test",
			Panels: []api.PanelResult{
				{
					PanelMeta: api.PanelMeta{
						Name:   "Active Pods",
						Type:   api.PanelTypeNumber,
						Number: &api.PanelNumberConfig{Unit: "pods", Precision: 0},
					},
					Rows: []dataquery.QueryResultRow{
						// Standard value field.
						{"value": 42, "label": "Production"},
						// count is the fallback when value is absent.
						{"count": 7, "label": "Staging"},
					},
				},
			},
		}

		html, err := views.RenderFacetHTML(context.New(), result, nil)
		Expect(err).ToNot(HaveOccurred())

		page := string(html)

		Expect(page).ToNot(ContainSubstring("Unsupported panel type"))

		// formatDisplayValue(42, "pods", 0) → "42 pods"
		Expect(page).To(ContainSubstring("42 pods"),
			"value field should be formatted with unit")
		// formatDisplayValue(7, "pods", 0) → "7 pods" — proves count fallback
		Expect(page).To(ContainSubstring("7 pods"),
			"count field should be used when value is absent")

		// Both labels must render — proves all rows are iterated, not just row[0].
		Expect(page).To(ContainSubstring("Production"))
		Expect(page).To(ContainSubstring("Staging"))
	})
})
