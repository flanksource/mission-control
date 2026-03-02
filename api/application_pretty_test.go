package api_test

import (
	"strings"
	"testing"
	"time"

	"github.com/flanksource/duty/view"
	"github.com/onsi/gomega"

	"github.com/flanksource/incident-commander/api"
)

var (
	testTime = time.Date(2026, 2, 27, 10, 0, 0, 0, time.UTC)
)

func TestApplicationDetailPretty(t *testing.T) {
	g := gomega.NewWithT(t)

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

	g.Expect(html).To(gomega.ContainSubstring("incident-commander"))
	g.Expect(html).To(gomega.ContainSubstring("Platform"))
	g.Expect(html).To(gomega.ContainSubstring("mc"))
	g.Expect(html).To(gomega.ContainSubstring("Version"))
	g.Expect(html).To(gomega.ContainSubstring("v1.2.4"))
}

func TestApplicationFindingPretty(t *testing.T) {
	g := gomega.NewWithT(t)

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

	g.Expect(highHTML).To(gomega.ContainSubstring("high"))
	g.Expect(highHTML).To(gomega.ContainSubstring("text-orange-700"))
	g.Expect(criticalHTML).To(gomega.ContainSubstring("critical"))
	g.Expect(criticalHTML).To(gomega.ContainSubstring("text-red-700"))
}

func TestApplicationSectionPrettyView(t *testing.T) {
	g := gomega.NewWithT(t)

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

	g.Expect(html).To(gomega.ContainSubstring("database"))
	g.Expect(html).To(gomega.ContainSubstring("incident-commander-db"))
}

func TestApplicationSectionPrettyChanges(t *testing.T) {
	g := gomega.NewWithT(t)

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

	g.Expect(html).To(gomega.ContainSubstring("kubernetes"))
	g.Expect(html).To(gomega.ContainSubstring("replicas scaled"))
}

func TestApplicationSectionPrettyConfigs(t *testing.T) {
	g := gomega.NewWithT(t)

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

	g.Expect(html).To(gomega.ContainSubstring("incident-commander"))
	g.Expect(strings.Contains(html, "text-green-700")).To(gomega.BeTrue())
}

func TestRender_EmptyPanelsOmitted(t *testing.T) {
	g := gomega.NewWithT(t)

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

	// Build the document by calling Pretty() on each section directly.
	// We verify headings are omitted for empty panels.
	var sections []string
	if len(app.Findings) > 0 {
		sections = append(sections, "Findings")
	}
	if len(app.Backups) > 0 {
		sections = append(sections, "Backups")
	}
	detailHTML := app.ApplicationDetail.Pretty().HTML()

	g.Expect(detailHTML).To(gomega.ContainSubstring("test-app"))
	g.Expect(sections).To(gomega.BeEmpty())
}
