package panelrender_test

import (
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/dataquery"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/views"
)

var _ = ginkgo.Describe("TextPanel", func() {
	ginkgo.It("renders row values as plain text", func() {
		result := &api.ViewResult{
			Name:  "text-panel-test",
			Title: "Text Panel Test",
			Panels: []api.PanelResult{
				{
					PanelMeta: api.PanelMeta{
						Name: "Release Notes",
						Type: api.PanelTypeText,
					},
					Rows: []dataquery.QueryResultRow{
						{"value": "Deployment pipeline completed successfully in production."},
						{"value": "Rollback window expires in 24 hours."},
					},
				},
			},
		}

		html, err := views.RenderFacetHTML(context.New(), result, nil)
		Expect(err).ToNot(HaveOccurred())

		page := string(html)

		Expect(page).ToNot(ContainSubstring("Unsupported panel type"))

		// Both row values must appear verbatim.
		Expect(page).To(ContainSubstring("Deployment pipeline completed successfully in production."))
		Expect(page).To(ContainSubstring("Rollback window expires in 24 hours."))
	})
})
