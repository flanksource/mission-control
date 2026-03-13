package api_test

import (
	"strings"
	"time"

	"github.com/flanksource/duty/view"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/incident-commander/api"
)

var (
	testTime = time.Date(2026, 2, 27, 10, 0, 0, 0, time.UTC)
)

var _ = ginkgo.Describe("Application Pretty", func() {
	ginkgo.It("should render application detail", func() {
		detail := api.ApplicationDetail{
			ID:          "a1b2c3d4",
			Name:        "incident-commander",
			Type:        "Platform",
			Namespace:   "mc",
			Description: "Flanksource Mission Control",
			CreatedAt:   testTime,
			Properties: []api.Property{
				{Name: "version", Label: "Version", Text: "v1.2.4"},
			},
		}

		html := detail.Pretty().HTML()

		Expect(html).To(ContainSubstring("incident-commander"))
		Expect(html).To(ContainSubstring("Platform"))
		Expect(html).To(ContainSubstring("mc"))
		Expect(html).To(ContainSubstring("Version"))
		Expect(html).To(ContainSubstring("v1.2.4"))
	})

	ginkgo.It("should render finding severity colors", func() {
		highFinding := api.ApplicationFinding{
			ID:           "f1",
			Type:         "security",
			Severity:     "high",
			Title:        "RDS publicly accessible",
			Description:  "PubliclyAccessible=true",
			Date:         testTime,
			LastObserved: testTime,
			Status:       "open",
		}
		criticalFinding := api.ApplicationFinding{
			ID:           "f2",
			Severity:     "critical",
			Title:        "SQL injection vulnerability",
			Status:       "open",
			Date:         testTime,
			LastObserved: testTime,
		}

		highHTML := highFinding.Pretty().HTML()
		criticalHTML := criticalFinding.Pretty().HTML()

		Expect(highHTML).To(ContainSubstring("high"))
		Expect(highHTML).To(ContainSubstring("text-orange-700"))
		Expect(criticalHTML).To(ContainSubstring("critical"))
		Expect(criticalHTML).To(ContainSubstring("text-red-700"))
	})

	ginkgo.It("should render view section with columns and rows", func() {
		refreshed := testTime
		section := api.ApplicationSection{
			Type:  api.SectionTypeView,
			Title: "Backups",
			View: &api.ApplicationViewData{
				RefreshStatus:   "fresh",
				LastRefreshedAt: &refreshed,
				Columns: []view.ColumnDef{
					{Name: "database", Type: view.ColumnTypeString},
					{Name: "status", Type: view.ColumnTypeStatus},
				},
				Rows: []view.Row{
					{"incident-commander-db", "success"},
				},
			},
		}

		html := section.Pretty().HTML()

		Expect(html).To(ContainSubstring("database"))
		Expect(html).To(ContainSubstring("incident-commander-db"))
	})

	ginkgo.It("should render changes section", func() {
		section := api.ApplicationSection{
			Type:  api.SectionTypeChanges,
			Title: "Recent Changes",
			Changes: []api.ApplicationChange{
				{
					ID:          "c1",
					Date:        testTime,
					Source:      "kubernetes",
					Description: "replicas scaled: 2 -> 3",
					Status:      "info",
					CreatedAt:   testTime,
				},
			},
		}

		html := section.Pretty().HTML()

		Expect(html).To(ContainSubstring("kubernetes"))
		Expect(html).To(ContainSubstring("replicas scaled"))
	})

	ginkgo.It("should render configs section with health color", func() {
		section := api.ApplicationSection{
			Type:  api.SectionTypeConfigs,
			Title: "Deployments",
			Configs: []api.ApplicationConfigItem{
				{
					ID:     "cfg1",
					Name:   "incident-commander",
					Type:   "Kubernetes::Deployment",
					Health: "healthy",
					Status: "Running",
					Labels: map[string]string{"app": "incident-commander"},
				},
			},
		}

		html := section.Pretty().HTML()

		Expect(html).To(ContainSubstring("incident-commander"))
		Expect(strings.Contains(html, "text-green-700")).To(BeTrue())
	})

	ginkgo.It("should omit empty panels", func() {
		app := &api.Application{
			ApplicationDetail: api.ApplicationDetail{
				Name:      "test-app",
				Namespace: "default",
				CreatedAt: testTime,
			},
			AccessControl: api.ApplicationAccessControl{},
			Findings:      nil,
			Backups:       nil,
		}

		detailHTML := app.ApplicationDetail.Pretty().HTML()
		Expect(detailHTML).To(ContainSubstring("test-app"))

		var rendered strings.Builder
		rendered.WriteString(detailHTML)
		if len(app.Findings) > 0 {
			rendered.WriteString("<h3>Security Findings</h3>")
			for _, f := range app.Findings {
				rendered.WriteString(f.Pretty().HTML())
			}
		}
		if len(app.Backups) > 0 {
			rendered.WriteString("<h3>Backups</h3>")
			for _, b := range app.Backups {
				rendered.WriteString(b.Pretty().HTML())
			}
		}

		html := rendered.String()

		Expect(html).ToNot(ContainSubstring("Security Findings"))
		Expect(html).ToNot(ContainSubstring("Backups"))
	})
})
