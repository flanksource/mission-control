package panelrender_test

import (
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/dataquery"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/views"
)

var _ = ginkgo.Describe("TimeseriesPanel", func() {
	ginkgo.It("infers time/value keys, formats timestamps, and renders as a table", func() {
		result := &api.ViewResult{
			Name:  "timeseries-panel-test",
			Title: "Timeseries Panel Test",
			Panels: []api.PanelResult{
				{
					PanelMeta: api.PanelMeta{
						Name: "Request Rate (5m)",
						Type: api.PanelTypeTimeseries,
						// No explicit timeKey/valueKey — the panel must infer them.
					},
					Rows: []dataquery.QueryResultRow{
						{"timestamp": "2026-03-24T20:00:00Z", "rps": 142},
						{"timestamp": "2026-03-24T20:05:00Z", "rps": 158},
						{"timestamp": "2026-03-24T20:10:00Z", "rps": 135},
					},
				},
			},
		}

		html, err := views.RenderFacetHTML(context.New(), result, nil)
		Expect(err).ToNot(HaveOccurred())

		page := string(html)

		Expect(page).ToNot(ContainSubstring("Unsupported panel type"))

		// Column headers "Time" and "Value" are set by the inference logic —
		// they only appear if timeKey and valueKey were successfully inferred.
		Expect(page).To(ContainSubstring("Time"),
			"'timestamp' key should be inferred as the time column")
		Expect(page).To(ContainSubstring("Value"),
			"'rps' key should be inferred as the value column")

		// ISO timestamps are reformatted by formatDateTime, which changes their
		// shape entirely. Asserting on "2026" proves the date was parsed and
		// re-rendered (not just passed through as the raw ISO string).
		Expect(page).To(ContainSubstring("2026"))
		Expect(page).ToNot(ContainSubstring("2026-03-24T20:00:00Z"),
			"raw ISO string should not appear — it must be reformatted")

		// Data values from all three rows must appear.
		Expect(page).To(ContainSubstring("142"))
		Expect(page).To(ContainSubstring("158"))
		Expect(page).To(ContainSubstring("135"))
	})
})
