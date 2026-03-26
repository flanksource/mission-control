package panelrender_test

import (
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/dataquery"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/views"
)

var _ = ginkgo.Describe("PropertiesPanel", func() {
	ginkgo.It("renders key-value pairs and falls back to first key as label", func() {
		result := &api.ViewResult{
			Name:  "properties-panel-test",
			Title: "Properties Panel Test",
			Panels: []api.PanelResult{
				{
					PanelMeta: api.PanelMeta{
						Name: "Cluster Info",
						Type: api.PanelTypeProperties,
					},
					Rows: []dataquery.QueryResultRow{
						// Standard: explicit label + value fields.
						{"label": "Region", "value": "us-east-1"},
						{"label": "K8s Version", "value": "1.29.3"},
						// Fallback: no label key — the value of the first non-value
						// key ("ap-southeast-2") should become the rendered label.
						{"cluster_region": "ap-southeast-2", "value": "active"},
					},
				},
			},
		}

		html, err := views.RenderFacetHTML(context.New(), result, nil)
		Expect(err).ToNot(HaveOccurred())

		page := string(html)

		Expect(page).ToNot(ContainSubstring("Unsupported panel type"))

		// Standard rows: label and value must both appear.
		Expect(page).To(ContainSubstring("Region"))
		Expect(page).To(ContainSubstring("us-east-1"))
		Expect(page).To(ContainSubstring("K8s Version"))
		Expect(page).To(ContainSubstring("1.29.3"))

		// Fallback row: the value of the first non-value key ("ap-southeast-2")
		// is used as the label — proves the alternate key extraction logic ran.
		Expect(page).To(ContainSubstring("ap-southeast-2"),
			"value of first non-value key should be used as label when label field is absent")
		Expect(page).To(ContainSubstring("active"))
	})
})
