package cmd

import (
	"time"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
)

var _ = ginkgo.Describe("buildCatalogGetOutput", func() {
	configID := uuid.New()
	since := 7 * 24 * time.Hour

	makeConfig := func() *models.ConfigItem {
		return &models.ConfigItem{
			ID:          configID,
			Name:        lo.ToPtr("my-deployment"),
			Type:        lo.ToPtr("Kubernetes::Deployment"),
			ConfigClass: "Deployment",
			Health:      lo.ToPtr(models.HealthHealthy),
		}
	}

	ginkgo.It("renders all sections when data is present", func() {
		configJSON := `{"replicas": 3}`
		c := makeConfig()
		c.Config = &configJSON
		r := CatalogGetResult{
			ConfigItem:     *c,
			since:          since.String(),
			showConfigJSON: true,
			Related: []query.RelatedConfig{
				{ID: uuid.New(), Name: "my-pod", Type: "Kubernetes::Pod", Relation: "outgoing", Health: lo.ToPtr(models.HealthHealthy)},
			},
			Insights: []models.ConfigAnalysis{
				{ID: uuid.New(), ConfigID: configID, Analyzer: "test-analyzer", AnalysisType: models.AnalysisTypeSecurity, Severity: models.SeverityHigh, Status: "open", Summary: "test finding"},
			},
			Changes: []models.ConfigChange{
				{ID: uuid.NewString(), ConfigID: configID.String(), ChangeType: "diff", Severity: models.SeverityInfo, Summary: "field changed", CreatedAt: lo.ToPtr(time.Now())},
			},
			Access: []models.ConfigAccessSummary{
				{ConfigID: configID, User: "alice", Role: "admin", Email: "alice@example.com", UserType: "user"},
			},
			PlaybookRuns: []models.PlaybookRun{
				{ID: uuid.New(), ConfigID: &configID, Status: models.PlaybookRunStatusCompleted, CreatedAt: time.Now()},
			},
		}

		out := r.Pretty().String()
		Expect(out).To(ContainSubstring("my-deployment"))
		Expect(out).To(ContainSubstring("Relationships"))
		Expect(out).To(ContainSubstring("Open Insights"))
		Expect(out).To(ContainSubstring("Changes since"))
		Expect(out).To(ContainSubstring("Access"))
		Expect(out).To(ContainSubstring("Playbook Runs"))
	})

	ginkgo.It("omits empty sections but always includes header and details", func() {
		r := CatalogGetResult{ConfigItem: *makeConfig(), since: since.String()}
		out := r.Pretty().String()
		Expect(out).To(ContainSubstring("my-deployment"))
		Expect(out).NotTo(ContainSubstring("Relationships"))
		Expect(out).NotTo(ContainSubstring("Open Insights"))
	})

	ginkgo.It("includes config code block when config JSON is present", func() {
		configJSON := `{"foo":"bar"}`
		c := makeConfig()
		c.Config = &configJSON
		r := CatalogGetResult{ConfigItem: *c, since: since.String(), showConfigJSON: true}
		out := r.Pretty().String()
		Expect(out).To(ContainSubstring("Config"))
		Expect(out).To(ContainSubstring("foo"))
	})
})

var _ = ginkgo.Describe("buildDetailsSection", func() {
	ginkgo.It("includes scraper and last scraped time", func() {
		scraperID := "scraper-123"
		lastScraped := time.Now().Add(-10 * time.Minute)
		r := CatalogGetResult{
			ConfigItem: models.ConfigItem{
				ID:        uuid.New(),
				Name:      lo.ToPtr("test"),
				Type:      lo.ToPtr("Kubernetes::Pod"),
				ScraperID: &scraperID,
			},
			LastScrapedTime: &lastScraped,
		}
		dl := buildDetailsSection(r)

		var foundScraper, foundLastScraped bool
		for _, item := range dl.Items {
			if item.Key == "Scraper" {
				foundScraper = true
				Expect(item.Value).To(Equal("scraper-123"))
			}
			if item.Key == "Last Scraped" {
				foundLastScraped = true
			}
		}
		Expect(foundScraper).To(BeTrue())
		Expect(foundLastScraped).To(BeTrue())
	})

	ginkgo.It("includes properties", func() {
		val := int64(42)
		r := CatalogGetResult{
			ConfigItem: models.ConfigItem{
				ID:   uuid.New(),
				Name: lo.ToPtr("test"),
				Type: lo.ToPtr("AWS::EC2::Instance"),
				Properties: &types.Properties{
					{Label: "Instance Type", Text: "t3.micro"},
					{Name: "cpu_count", Value: &val},
				},
			},
		}
		dl := buildDetailsSection(r)

		var foundInstanceType, foundCPU bool
		for _, item := range dl.Items {
			if item.Key == "Instance Type" {
				foundInstanceType = true
				Expect(item.Value).To(Equal("t3.micro"))
			}
			if item.Key == "cpu_count" {
				foundCPU = true
				Expect(item.Value).To(Equal("42"))
			}
		}
		Expect(foundInstanceType).To(BeTrue())
		Expect(foundCPU).To(BeTrue())
	})
})

var _ = ginkgo.Describe("buildRelationshipTree", func() {
	ginkgo.It("builds parent-child tree from paths", func() {
		rootID := uuid.New()
		childID := uuid.New()
		grandchildID := uuid.New()
		config := &models.ConfigItem{
			ID:   rootID,
			Name: lo.ToPtr("root"),
			Type: lo.ToPtr("Kubernetes::Deployment"),
		}
		related := []query.RelatedConfig{
			{ID: childID, Name: "child", Type: "Kubernetes::Pod", Path: rootID.String() + "." + childID.String()},
			{ID: grandchildID, Name: "grandchild", Type: "Kubernetes::Container", Path: rootID.String() + "." + childID.String() + "." + grandchildID.String()},
		}
		tree := buildRelationshipTree(config, related)
		// root has 1 child, that child has 1 grandchild
		Expect(tree.Children).To(HaveLen(1))
		Expect(tree.Children[0].Children).To(HaveLen(1))
	})

	ginkgo.It("attaches orphans to root", func() {
		rootID := uuid.New()
		orphanID := uuid.New()
		config := &models.ConfigItem{
			ID:   rootID,
			Name: lo.ToPtr("root"),
			Type: lo.ToPtr("Kubernetes::Deployment"),
		}
		related := []query.RelatedConfig{
			{ID: orphanID, Name: "orphan", Type: "Kubernetes::Pod", Path: ""},
		}
		tree := buildRelationshipTree(config, related)
		Expect(tree.Children).To(HaveLen(1))
	})
})

var _ = ginkgo.Describe("parentIDFromPath", func() {
	for _, tt := range []struct {
		name     string
		path     string
		id       string
		expected string
	}{
		{"middle of path", "a.b.c", "c", "b"},
		{"root child", "a.b", "b", "a"},
		{"first element", "a.b", "a", ""},
		{"empty path", "", "a", ""},
		{"not in path", "a.b.c", "d", ""},
	} {
		ginkgo.It(tt.name, func() {
			Expect(parentIDFromPath(tt.path, tt.id)).To(Equal(tt.expected))
		})
	}
})

var _ = ginkgo.Describe("configCodeBlock", func() {
	ginkgo.It("pretty prints JSON", func() {
		code := configCodeBlock(`{"a":1,"b":"c"}`)
		Expect(code.String()).To(ContainSubstring("\"a\": 1"))
	})
})

var _ = ginkgo.Describe("formatDuration", func() {
	for _, tt := range []struct {
		name     string
		input    time.Duration
		expected string
	}{
		{"7 days", 7 * 24 * time.Hour, "7d"},
		{"1 day", 24 * time.Hour, "1d"},
		{"30 days", 30 * 24 * time.Hour, "30d"},
		{"sub-day", 6 * time.Hour, "6h0m0s"},
	} {
		ginkgo.It(tt.name, func() {
			Expect(formatDuration(tt.input)).To(Equal(tt.expected))
		})
	}
})

var _ = ginkgo.Describe("TableProvider wrappers", func() {
	ginkgo.It("analysisRow returns correct columns", func() {
		r := analysisRow{models.ConfigAnalysis{Analyzer: "test", Severity: models.SeverityHigh}}
		Expect(r.Columns()).To(HaveLen(5))
		row := r.Row()
		Expect(row).To(HaveKey("Severity"))
		Expect(row).To(HaveKey("Analyzer"))
	})

	ginkgo.It("accessRow handles nil LastSignedInAt", func() {
		r := accessRow{models.ConfigAccessSummary{User: "bob", Role: "viewer"}}
		row := r.Row()
		Expect(row).To(HaveKey("LastSignedIn"))
	})

	ginkgo.It("playbookRunRow computes duration", func() {
		start := time.Now().Add(-5 * time.Minute)
		end := time.Now()
		r := playbookRunRow{models.PlaybookRun{
			ID:        uuid.New(),
			Status:    models.PlaybookRunStatusCompleted,
			CreatedAt: start,
			StartTime: &start,
			EndTime:   &end,
		}}
		row := r.Row()
		Expect(row).To(HaveKey("Duration"))
		Expect(row).To(HaveKey("Status"))
	})
})
