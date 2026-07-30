package catalog

import (
	"net/http"
	"net/http/httptest"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	reportCatalog "github.com/flanksource/incident-commander/report/catalog"
)

var _ = ginkgo.Describe("config relationships", func() {
	ginkgo.It("rejects invalid config ids", func() {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/catalog/not-a-uuid/relationships", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues("not-a-uuid")

		Expect(GetConfigRelationships(c)).To(Succeed())
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
	})

	ginkgo.It("normalizes a tree to the requested config node and preserves health and status", func() {
		parentID := uuid.New()
		targetID := uuid.New()
		childID := uuid.New()
		health := models.HealthWarning
		status := "degraded"

		tree := &query.ConfigTreeNode{
			ConfigItem: models.ConfigItem{ID: parentID, Name: ptr("cluster")},
			EdgeType:   "parent",
			Children: []*query.ConfigTreeNode{
				{
					ConfigItem: models.ConfigItem{
						ID:     targetID,
						Name:   ptr("helm-release"),
						Health: &health,
						Status: &status,
					},
					EdgeType: "target",
					Children: []*query.ConfigTreeNode{
						{
							ConfigItem: models.ConfigItem{ID: childID, Name: ptr("deployment")},
							EdgeType:   "child",
						},
					},
				},
			},
		}

		root := findConfigTreeNode(tree, targetID)

		Expect(root).NotTo(BeNil())
		Expect(root.ID).To(Equal(targetID))
		Expect(root.EdgeType).To(Equal("target"))
		Expect(root.Health).To(Equal(&health))
		Expect(root.Status).To(Equal(&status))
		Expect(root.Children).To(HaveLen(1))
		Expect(root.Children[0].ID).To(Equal(childID))
	})
})

var _ = ginkgo.Describe("bulk config item delete", func() {
	ginkgo.It("normalizes and deduplicates config item ids", func() {
		first := uuid.New()
		second := uuid.New()

		ids, err := normalizeConfigItemIDs([]string{
			" " + first.String() + " ",
			first.String(),
			second.String(),
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(ids).To(Equal([]string{first.String(), second.String()}))
	})

	ginkgo.It("rejects empty and invalid config item ids", func() {
		_, err := normalizeConfigItemIDs(nil)
		Expect(err).To(HaveOccurred())

		_, err = normalizeConfigItemIDs([]string{""})
		Expect(err).To(HaveOccurred())

		_, err = normalizeConfigItemIDs([]string{"not-a-uuid"})
		Expect(err).To(HaveOccurred())
	})
})

var _ = ginkgo.Describe("catalog report endpoints", func() {
	ginkgo.It("normalizes report formats to response metadata", func() {
		format, contentType, extension, err := normalizeCatalogReportFormat("")
		Expect(err).NotTo(HaveOccurred())
		Expect(format).To(Equal("facet-pdf"))
		Expect(contentType).To(Equal("application/pdf"))
		Expect(extension).To(Equal("pdf"))

		format, contentType, extension, err = normalizeCatalogReportFormat("facet-html")
		Expect(err).NotTo(HaveOccurred())
		Expect(format).To(Equal("facet-html"))
		Expect(contentType).To(Equal("text/html; charset=utf-8"))
		Expect(extension).To(Equal("html"))

		_, _, _, err = normalizeCatalogReportFormat("docx")
		Expect(err).To(HaveOccurred())
	})

	ginkgo.It("builds a unified tree from selected resources", func() {
		rootID := uuid.New()
		childID := uuid.New()
		grandchildID := uuid.New()

		forest := buildConfigForest([]models.ConfigItem{
			{ID: grandchildID, Name: ptr("pod"), Type: ptr("Kubernetes::Pod"), ParentID: &childID, Path: rootID.String() + "." + childID.String() + "." + grandchildID.String()},
			{ID: childID, Name: ptr("deployment"), Type: ptr("Kubernetes::Deployment"), ParentID: &rootID, Path: rootID.String() + "." + childID.String()},
			{ID: rootID, Name: ptr("namespace"), Type: ptr("Kubernetes::Namespace"), Path: rootID.String()},
		})

		Expect(forest).To(HaveLen(1))
		Expect(forest[0].ID).To(Equal(rootID))
		Expect(forest[0].Children).To(HaveLen(1))
		Expect(forest[0].Children[0].ID).To(Equal(childID))
		Expect(forest[0].Children[0].Children).To(HaveLen(1))
		Expect(forest[0].Children[0].Children[0].ID).To(Equal(grandchildID))
		Expect(flattenConfigTreeIDs(forest)).To(Equal([]string{
			rootID.String(),
			childID.String(),
			grandchildID.String(),
		}))
	})

	ginkgo.It("uses CLI-compatible report option defaults", func() {
		opts, err := catalogReportOptionsFromRequest(CatalogReportRequest{})
		Expect(err).NotTo(HaveOccurred())
		Expect(opts.Sections.Changes).To(BeTrue())
		Expect(opts.Sections.Insights).To(BeTrue())
		Expect(opts.Sections.Relationships).To(BeTrue())
		Expect(opts.Sections.Access).To(BeTrue())
		Expect(opts.Sections.AccessLogs).To(BeFalse())
		Expect(opts.Sections.ConfigJSON).To(BeFalse())
		Expect(opts.Settings).NotTo(BeNil())
	})

	ginkgo.It("builds a stable download filename", func() {
		Expect(catalogReportFilename("Production / Billing", "pdf")).To(Equal("production-billing.pdf"))
		Expect(catalogReportFilename("", "json")).To(Equal("catalog-report.json"))
	})

	ginkgo.Describe("selection", func() {
		var createdIDs []uuid.UUID

		ginkgo.AfterEach(func() {
			if len(createdIDs) > 0 {
				Expect(DefaultContext.DB().Unscoped().Delete(&models.ConfigItem{}, "id IN ?", createdIDs).Error).To(Succeed())
			}
		})

		createConfigs := func(configs ...models.ConfigItem) []models.ConfigItem {
			Expect(DefaultContext.DB().Create(&configs).Error).To(Succeed())
			for _, config := range configs {
				createdIDs = append(createdIDs, config.ID)
			}
			return configs
		}

		ginkgo.It("expands only roots with includeChildren", func() {
			firstRoot := catalogConfig("first-root", nil)
			firstChild := catalogConfig("first-child", &firstRoot)
			secondRoot := catalogConfig("second-root", nil)
			secondChild := catalogConfig("second-child", &secondRoot)
			createConfigs(firstRoot, firstChild, secondRoot, secondChild)

			selection, opts, err := resolveCatalogReportSelection(DefaultContext, CatalogReportRequest{
				Roots: []CatalogReportRoot{
					{ID: firstRoot.ID.String(), IncludeChildren: true},
					{ID: secondRoot.ID.String(), IncludeChildren: false},
				},
			}, []models.ConfigItem{firstRoot, secondRoot}, reportCatalog.Options{Settings: &reportCatalog.Settings{}})

			Expect(err).NotTo(HaveOccurred())
			Expect(opts.Recursive).To(BeTrue())
			Expect(configIDs(selection.Items)).To(ConsistOf(firstRoot.ID, firstChild.ID, secondRoot.ID))
			Expect(configIDs(selection.ItemsByRoot[firstRoot.ID])).To(ConsistOf(firstRoot.ID, firstChild.ID))
			Expect(configIDs(selection.ItemsByRoot[secondRoot.ID])).To(Equal([]uuid.UUID{secondRoot.ID}))
		})

		ginkgo.It("retains explicit roots while filtering recursive descendants", func() {
			root := catalogConfig("filter-root", nil)
			included := catalogConfig("keep-child", &root)
			excluded := catalogConfig("drop-child", &root)
			createConfigs(root, included, excluded)

			selection, _, err := resolveCatalogReportSelection(DefaultContext, CatalogReportRequest{
				Roots: []CatalogReportRoot{{ID: root.ID.String(), IncludeChildren: true}},
			}, []models.ConfigItem{root}, reportCatalog.Options{
				Settings: &reportCatalog.Settings{Filters: []string{"name=keep-child"}},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(configIDs(selection.Items)).To(ConsistOf(root.ID, included.ID))
			Expect(configIDs(selection.Items)).NotTo(ContainElement(excluded.ID))
		})

		ginkgo.It("assigns overlapping descendants to the nearest explicit root", func() {
			outer := catalogConfig("outer-root", nil)
			inner := catalogConfig("inner-root", &outer)
			child := catalogConfig("owned-child", &inner)
			createConfigs(outer, inner, child)

			selection, _, err := resolveCatalogReportSelection(DefaultContext, CatalogReportRequest{
				Recursive: true,
			}, []models.ConfigItem{outer, inner}, reportCatalog.Options{
				Recursive: true,
				Settings:  &reportCatalog.Settings{},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(configIDs(selection.Items)).To(ConsistOf(outer.ID, inner.ID, child.ID))
			Expect(configIDs(selection.ItemsByRoot[outer.ID])).To(Equal([]uuid.UUID{outer.ID}))
			Expect(configIDs(selection.ItemsByRoot[inner.ID])).To(ConsistOf(inner.ID, child.ID))
		})
	})
})

func catalogConfig(name string, parent *models.ConfigItem) models.ConfigItem {
	id := uuid.New()
	configType := "Test::CatalogReport"
	config := models.ConfigItem{ID: id, Name: &name, Type: &configType, Path: id.String()}
	if parent != nil {
		config.ParentID = &parent.ID
		config.Path = parent.Path + "." + id.String()
	}
	return config
}

func configIDs(configs []models.ConfigItem) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(configs))
	for _, config := range configs {
		ids = append(ids, config.ID)
	}
	return ids
}

func ptr(value string) *string {
	return &value
}
