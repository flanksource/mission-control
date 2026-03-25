package panelrender_test

import (
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/dataquery"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/views"
)

var _ = ginkgo.Describe("HeatmapPanel", func() {
	ginkgo.It("calendar mode: renders day headers, month label, and colors cells by success/failure status", func() {
		result := &api.ViewResult{
			Name:  "heatmap-panel-test",
			Title: "Heatmap Panel Test",
			Panels: []api.PanelResult{
				{
					PanelMeta: api.PanelMeta{
						Name:    "Backup History",
						Type:    api.PanelTypeHeatmap,
						Heatmap: &api.PanelHeatmapConfig{},
					},
					Rows: []dataquery.QueryResultRow{
						// Standard 'date' key — successful run.
						{"date": "2026-03-10", "successful": 3, "failed": 0},
						// 'day' alias — failed run.
						{"day": "2026-03-15", "successful": 0, "failed": 2},
						// 'timestamp' alias with full ISO string — count-only row.
						// The renderer must truncate the timestamp to a date and treat
						// the entire count as successful when successful/failed are absent.
						{"timestamp": "2026-03-20T10:30:00Z", "count": 5},
					},
				},
			},
		}

		html, err := views.RenderFacetHTML(context.New(), result, nil)
		Expect(err).ToNot(HaveOccurred())

		page := string(html)

		Expect(page).ToNot(ContainSubstring("Unsupported panel type"))

		// Day-of-week headers prove the calendar grid structure was rendered.
		Expect(page).To(ContainSubstring("Su"),
			"calendar day-of-week headers must be rendered")
		Expect(page).To(ContainSubstring("Mo"),
			"calendar day-of-week headers must be rendered")

		// Month label proves the date was parsed and localised correctly.
		Expect(page).To(ContainSubstring("March"),
			"month name should appear in the calendar header")
		Expect(page).To(ContainSubstring("2026"),
			"year should appear in the calendar header")

		// bg-green-50 is the cell background for a successful day; it can only
		// appear if getCalendarStatus() returned 'success'.
		Expect(page).To(ContainSubstring("bg-green-50"),
			"successful days must receive the green cell class")

		// bg-red-50 is the cell background for a failed day.
		Expect(page).To(ContainSubstring("bg-red-50"),
			"failed days must receive the red cell class")

		// text-green-700 is applied to the count label inside a successful cell;
		// its presence proves getCellLabel() returned a non-empty string for the
		// successful run (3 total).
		Expect(page).To(ContainSubstring("text-green-700"),
			"successful cell must render a count label in the green text class")

		// text-red-600 is applied to the count label inside a failed cell.
		Expect(page).To(ContainSubstring("text-red-600"),
			"failed cell must render a count label in the red text class")

		// The tooltip for the timestamp-aliased row proves two things at once:
		// 1. The full ISO timestamp was correctly truncated to "2026-03-20".
		// 2. The count-only fallback ran: count=5 was promoted to successful=5.
		Expect(page).To(ContainSubstring("2026-03-20: 5 successful, 0 failed (5 total)"),
			"timestamp alias and count-only fallback must both be applied")

		// The tooltip for the 'day'-aliased row proves the day alias was resolved.
		Expect(page).To(ContainSubstring("2026-03-15: 0 successful, 2 failed (2 total)"),
			"day alias must be resolved to a date key")

		// Legend items confirm the full calendar legend block was rendered.
		Expect(page).To(ContainSubstring("No backup"),
			"calendar legend must include the 'No backup' entry")
	})

	ginkgo.It("compact mode: colors cells by success, mixed, and failed ratios", func() {
		result := &api.ViewResult{
			Name:  "heatmap-compact-panel-test",
			Title: "Heatmap Compact Panel Test",
			Panels: []api.PanelResult{
				{
					PanelMeta: api.PanelMeta{
						Name:    "CI Run History",
						Type:    api.PanelTypeHeatmap,
						Heatmap: &api.PanelHeatmapConfig{Mode: api.HeatmapVariantCompact},
					},
					Rows: []dataquery.QueryResultRow{
						// All successful → success cell (#BBF7D0).
						{"date": "2026-03-10", "successful": 5, "failed": 0},
						// successRatio = 3/4 = 0.75 ≥ 0.5 → mixed cell (#FED7AA).
						{"date": "2026-03-11", "successful": 3, "failed": 1},
						// successRatio = 1/5 = 0.2 < 0.5 → failed cell (#FECACA).
						{"date": "2026-03-12", "successful": 1, "failed": 4},
					},
				},
			},
		}

		html, err := views.RenderFacetHTML(context.New(), result, nil)
		Expect(err).ToNot(HaveOccurred())

		page := string(html)

		Expect(page).ToNot(ContainSubstring("Unsupported panel type"))

		// Cell background colors prove getCompactCellKind() returned the correct
		// variant for each row's success ratio.
		Expect(page).To(ContainSubstring("#BBF7D0"),
			"all-successful row must render with the success background color")
		Expect(page).To(ContainSubstring("#FED7AA"),
			"row with successRatio≥0.5 must render with the mixed background color")
		Expect(page).To(ContainSubstring("#FECACA"),
			"row with successRatio<0.5 must render with the failed background color")

		// Tooltip text for the all-successful row proves the value was built correctly.
		Expect(page).To(ContainSubstring("2026-03-10: 5 successful, 0 failed (5 total)"),
			"compact cell tooltip must show the correct counts")

		// Legend items confirm the full compact legend block was rendered.
		Expect(page).To(ContainSubstring("Mixed"),
			"compact legend must include the 'Mixed' entry")
	})
})
