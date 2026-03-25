package panelrender_test

import (
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/dataquery"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/views"
)

var _ = ginkgo.Describe("DurationPanel", func() {
	// One render call covers all rows — facet invocations are expensive.
	ginkgo.It("converts nanoseconds to human-readable durations", func() {
		result := &api.ViewResult{
			Name:  "duration-panel-test",
			Title: "Duration Panel Test",
			Panels: []api.PanelResult{
				{
					PanelMeta: api.PanelMeta{
						Name: "Pipeline Durations",
						Type: api.PanelTypeDuration,
					},
					Rows: []dataquery.QueryResultRow{
						// 127_000_000_000 ns → 127_000 ms → "2m 7s"
						{"value": int64(127_000_000_000), "label": "production-deploy"},
						// 45_000_000_000 ns → 45_000 ms → "45.0s"
						{"value": int64(45_000_000_000), "label": "staging-deploy"},
						// 500_000_000 ns → 500 ms → "500ms"
						{"value": int64(500_000_000), "label": "smoke-test"},
						// 3_900_000_000_000 ns → 3_900_000 ms → "1h 5m"
						{"value": int64(3_900_000_000_000), "label": "full-suite"},
					},
				},
			},
		}

		html, err := views.RenderFacetHTML(context.New(), result, nil)
		Expect(err).ToNot(HaveOccurred())

		page := string(html)

		// No panel should fall through to the default switch case.
		Expect(page).ToNot(ContainSubstring("Unsupported panel type"))

		// Each value is a computed output, not raw input — proves the
		// ns → ms → human conversion ran correctly for each branch.
		Expect(page).To(ContainSubstring("2m 7s"),
			"127_000_000_000 ns should format to 2m 7s")
		Expect(page).To(ContainSubstring("45.0s"),
			"45_000_000_000 ns should format to 45.0s")
		Expect(page).To(ContainSubstring("500ms"),
			"500_000_000 ns should format to 500ms")
		Expect(page).To(ContainSubstring("1h 5m"),
			"3_900_000_000_000 ns should format to 1h 5m")

		// All four labels must appear — proves every row rendered,
		// not just the first (the old single-row bug pattern).
		Expect(page).To(ContainSubstring("production-deploy"))
		Expect(page).To(ContainSubstring("staging-deploy"))
		Expect(page).To(ContainSubstring("smoke-test"))
		Expect(page).To(ContainSubstring("full-suite"))
	})
})
