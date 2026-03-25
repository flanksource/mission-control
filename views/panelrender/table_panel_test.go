package panelrender_test

import (
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/dataquery"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/views"
)

var _ = ginkgo.Describe("TablePanel", func() {
	ginkgo.It("renders row keys as title-cased headers and all rows", func() {
		result := &api.ViewResult{
			Name:  "table-panel-test",
			Title: "Table Panel Test",
			Panels: []api.PanelResult{
				{
					PanelMeta: api.PanelMeta{
						Name: "Top Namespaces",
						Type: api.PanelTypeTable,
					},
					Rows: []dataquery.QueryResultRow{
						{"namespace": "kube-system", "pods": "12", "status": "Healthy"},
						{"namespace": "monitoring", "pods": "8", "status": "Degraded"},
						{"namespace": "default", "pods": "3", "status": "Healthy"},
					},
				},
			},
		}

		html, err := views.RenderFacetHTML(context.New(), result, nil)
		Expect(err).ToNot(HaveOccurred())

		page := string(html)

		Expect(page).ToNot(ContainSubstring("Unsupported panel type"))

		// Keys must be title-cased and used as column headers — this is the
		// computed transformation (k.replace(/_/g,'').replace(/\b\w/g,...)).
		Expect(page).To(ContainSubstring("Namespace"))
		Expect(page).To(ContainSubstring("Pods"))
		Expect(page).To(ContainSubstring("Status"))

		// All three rows must render, not just the first.
		Expect(page).To(ContainSubstring("kube-system"))
		Expect(page).To(ContainSubstring("monitoring"))
		Expect(page).To(ContainSubstring("default"))
	})
})
