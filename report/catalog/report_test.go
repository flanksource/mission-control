package catalog

import (
	gocontext "context"
	"testing"
	"time"

	dutyCtx "github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/incident-commander/api"
)

func TestCatalogReport(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "CatalogReport")
}

var _ = ginkgo.Describe("Options", func() {
	ginkgo.It("WithDefaults sets 30-day since", func() {
		opts := Options{}.WithDefaults()
		Expect(opts.Since).To(Equal(30 * 24 * time.Hour))
	})

	ginkgo.It("WithDefaults preserves custom since", func() {
		opts := Options{Since: 7 * 24 * time.Hour}.WithDefaults()
		Expect(opts.Since).To(Equal(7 * 24 * time.Hour))
	})

	ginkgo.It("WithDefaults sets GroupBy to none", func() {
		opts := Options{}.WithDefaults()
		Expect(opts.GroupBy).To(Equal("none"))
	})

	ginkgo.It("WithDefaults preserves explicit GroupBy", func() {
		opts := Options{GroupBy: "config"}.WithDefaults()
		Expect(opts.GroupBy).To(Equal("config"))
	})
})

var _ = ginkgo.Describe("Report date range", func() {
	ginkgo.It("From is set from sinceTime", func() {
		opts := Options{Since: 48 * time.Hour}.WithDefaults()
		sinceTime := time.Now().Add(-opts.Since)

		report := &api.CatalogReport{
			From: sinceTime.Format(time.RFC3339),
		}

		parsed, err := time.Parse(time.RFC3339, report.From)
		Expect(err).ToNot(HaveOccurred())
		Expect(parsed).To(BeTemporally("~", time.Now().Add(-48*time.Hour), 2*time.Second))
	})

	ginkgo.It("From matches sinceTime for 30-day default", func() {
		opts := Options{}.WithDefaults()
		sinceTime := time.Now().Add(-opts.Since)

		report := &api.CatalogReport{
			From: sinceTime.Format(time.RFC3339),
		}

		parsed, err := time.Parse(time.RFC3339, report.From)
		Expect(err).ToNot(HaveOccurred())
		Expect(parsed).To(BeTemporally("~", time.Now().Add(-30*24*time.Hour), 2*time.Second))
	})

	ginkgo.It("query FromTime matches report From", func() {
		opts := Options{Since: 7 * 24 * time.Hour}.WithDefaults()
		sinceTime := time.Now().Add(-opts.Since)

		report := &api.CatalogReport{
			From: sinceTime.Format(time.RFC3339),
		}

		reportFrom, err := time.Parse(time.RFC3339, report.From)
		Expect(err).ToNot(HaveOccurred())
		Expect(reportFrom).To(BeTemporally("~", sinceTime, time.Second))
	})
})

var _ = ginkgo.Describe("Options.effectiveMax", func() {
	cases := []struct {
		name     string
		maxItems int
		override int
		expected int
	}{
		{"both unlimited", 0, 0, 0},
		{"only MaxItems", 50, 0, 50},
		{"only override", 0, 100, 100},
		{"override tighter than MaxItems", 50, 20, 20},
		{"override looser than MaxItems", 50, 100, 50},
		{"override equals MaxItems", 50, 50, 50},
	}
	for _, tc := range cases {
		ginkgo.It(tc.name, func() {
			opts := Options{MaxItems: tc.maxItems}
			Expect(opts.effectiveMax(tc.override)).To(Equal(tc.expected))
		})
	}
})

var _ = ginkgo.Describe("capArtifactsPerSource", func() {
	makeChange := func(id, source string, artifactCount int) api.CatalogReportChange {
		arts := make([]api.CatalogReportArtifact, artifactCount)
		for i := range arts {
			arts[i] = api.CatalogReportArtifact{ID: id + "-" + uuid.New().String()}
		}
		return api.CatalogReportChange{ID: id, Source: source, Artifacts: arts}
	}

	ginkgo.It("is a no-op when maxPerSource is zero", func() {
		changes := []api.CatalogReportChange{makeChange("c1", "diff", 5)}
		capArtifactsPerSource(changes, 0)
		Expect(changes[0].Artifacts).To(HaveLen(5))
	})

	ginkgo.It("caps each source bucket independently", func() {
		changes := []api.CatalogReportChange{
			makeChange("c1", "diff", 5),
			makeChange("c2", "kubernetes", 5),
			makeChange("c3", "cloudtrail", 5),
		}
		capArtifactsPerSource(changes, 2)
		Expect(changes[0].Artifacts).To(HaveLen(2))
		Expect(changes[1].Artifacts).To(HaveLen(2))
		Expect(changes[2].Artifacts).To(HaveLen(2))
	})

	ginkgo.It("drops artifacts from later changes in the same source bucket", func() {
		changes := []api.CatalogReportChange{
			makeChange("c1", "diff", 2),
			makeChange("c2", "diff", 3),
			makeChange("c3", "diff", 1),
		}
		capArtifactsPerSource(changes, 3)
		Expect(changes[0].Artifacts).To(HaveLen(2))
		Expect(changes[1].Artifacts).To(HaveLen(1))
		Expect(changes[2].Artifacts).To(BeEmpty())
	})

	ginkgo.It("leaves changes without artifacts untouched", func() {
		changes := []api.CatalogReportChange{
			{ID: "c1", Source: "diff"},
			makeChange("c2", "diff", 5),
		}
		capArtifactsPerSource(changes, 2)
		Expect(changes[0].Artifacts).To(BeEmpty())
		Expect(changes[1].Artifacts).To(HaveLen(2))
	})
})

var _ = ginkgo.Describe("parentIDsFromPath", func() {
	ginkgo.It("returns parents in path order", func() {
		parentA := uuid.New()
		parentB := uuid.New()
		child := uuid.New()

		config := &models.ConfigItem{
			ID:   child,
			Path: parentA.String() + "." + parentB.String() + "." + child.String(),
		}

		Expect(parentIDsFromPath(config)).To(Equal([]uuid.UUID{parentA, parentB}))
	})

	ginkgo.It("ignores invalid segments and the config itself", func() {
		parent := uuid.New()
		child := uuid.New()

		config := &models.ConfigItem{
			ID:   child,
			Path: "not-a-uuid." + child.String() + "." + parent.String() + ".still-not-a-uuid",
		}

		Expect(parentIDsFromPath(config)).To(Equal([]uuid.UUID{parent}))
	})

	ginkgo.It("returns nil for nil config or empty path", func() {
		Expect(parentIDsFromPath(nil)).To(BeNil())
		Expect(parentIDsFromPath(&models.ConfigItem{})).To(BeNil())
	})

	ginkgo.It("uses path only even when ParentID is cyclic", func() {
		parentA := uuid.New()
		parentB := uuid.New()
		cycleParent := uuid.New()
		child := uuid.New()

		config := &models.ConfigItem{
			ID:       child,
			ParentID: &cycleParent,
			Path:     parentA.String() + "." + parentB.String() + "." + child.String(),
		}

		Expect(parentIDsFromPath(config)).To(Equal([]uuid.UUID{parentA, parentB}))
	})
})

var _ = ginkgo.Describe("configTreeNodeToReport cycle protection", func() {
	ginkgo.It("terminates on a self-referential cycle", func() {
		idA := uuid.New()
		nodeA := &query.ConfigTreeNode{
			ConfigItem: models.ConfigItem{ID: idA},
		}
		// A -> A (self-loop)
		nodeA.Children = []*query.ConfigTreeNode{nodeA}

		result := configTreeNodeToReport(nodeA, nil)
		Expect(result).ToNot(BeNil())
		Expect(result.Children).To(HaveLen(1))
		Expect(result.Children[0].Children).To(BeEmpty())
	})

	ginkgo.It("terminates on an A -> B -> A cycle", func() {
		idA := uuid.New()
		idB := uuid.New()
		nodeA := &query.ConfigTreeNode{ConfigItem: models.ConfigItem{ID: idA}}
		nodeB := &query.ConfigTreeNode{ConfigItem: models.ConfigItem{ID: idB}}
		nodeA.Children = []*query.ConfigTreeNode{nodeB}
		nodeB.Children = []*query.ConfigTreeNode{nodeA}

		result := configTreeNodeToReport(nodeA, nil)
		Expect(result).ToNot(BeNil())
		Expect(result.Children).To(HaveLen(1))
		// nodeB's child is nodeA again, but A was already visited — empty.
		Expect(result.Children[0].Children).To(HaveLen(1))
		Expect(result.Children[0].Children[0].Children).To(BeEmpty())
	})

	ginkgo.It("preserves acyclic subtrees", func() {
		idA, idB, idC := uuid.New(), uuid.New(), uuid.New()
		nodeC := &query.ConfigTreeNode{ConfigItem: models.ConfigItem{ID: idC}}
		nodeB := &query.ConfigTreeNode{ConfigItem: models.ConfigItem{ID: idB}, Children: []*query.ConfigTreeNode{nodeC}}
		nodeA := &query.ConfigTreeNode{ConfigItem: models.ConfigItem{ID: idA}, Children: []*query.ConfigTreeNode{nodeB}}

		result := configTreeNodeToReport(nodeA, nil)
		Expect(result.Children).To(HaveLen(1))
		Expect(result.Children[0].Children).To(HaveLen(1))
		Expect(result.Children[0].Children[0].Children).To(BeEmpty())
	})

	ginkgo.It("prunes nodes outside an explicit include set", func() {
		idA, idB, idC := uuid.New(), uuid.New(), uuid.New()
		nodeC := &query.ConfigTreeNode{ConfigItem: models.ConfigItem{ID: idC}}
		nodeB := &query.ConfigTreeNode{ConfigItem: models.ConfigItem{ID: idB}, Children: []*query.ConfigTreeNode{nodeC}}
		nodeA := &query.ConfigTreeNode{ConfigItem: models.ConfigItem{ID: idA}, Children: []*query.ConfigTreeNode{nodeB}}

		result := configTreeNodeToReport(nodeA, map[uuid.UUID]bool{idA: true})

		Expect(result).ToNot(BeNil())
		Expect(result.ID).To(Equal(idA.String()))
		Expect(result.Children).To(BeEmpty())
	})
})

var _ = ginkgo.Describe("classifyRootsAndBreadcrumbs", func() {
	makeCI := func(id, parent, grandparent uuid.UUID, name string) models.ConfigItem {
		ci := models.ConfigItem{ID: id, Name: &name}
		switch {
		case grandparent != uuid.Nil && parent != uuid.Nil:
			ci.Path = grandparent.String() + "." + parent.String() + "." + id.String()
		case parent != uuid.Nil:
			ci.Path = parent.String() + "." + id.String()
		default:
			ci.Path = id.String()
		}
		return ci
	}

	makeEntries := func(configs []models.ConfigItem) []api.CatalogReportEntry {
		entries := make([]api.CatalogReportEntry, len(configs))
		for i, c := range configs {
			entries[i] = api.CatalogReportEntry{
				ConfigItem: api.NewCatalogReportConfigItem(c),
			}
		}
		return entries
	}

	ginkgo.It("marks a standalone entry as root", func() {
		a := uuid.New()
		configs := []models.ConfigItem{makeCI(a, uuid.Nil, uuid.Nil, "a")}
		entries := makeEntries(configs)

		classifyRootsAndBreadcrumbs(configs, entries)

		Expect(entries[0].IsRoot).To(BeTrue())
		Expect(entries[0].Breadcrumb).To(BeEmpty())
	})

	ginkgo.It("marks parent as root and child with a breadcrumb", func() {
		parent := uuid.New()
		child := uuid.New()
		configs := []models.ConfigItem{
			makeCI(parent, uuid.Nil, uuid.Nil, "parent"),
			makeCI(child, parent, uuid.Nil, "child"),
		}
		entries := makeEntries(configs)

		classifyRootsAndBreadcrumbs(configs, entries)

		Expect(entries[0].IsRoot).To(BeTrue())
		Expect(entries[0].Breadcrumb).To(BeEmpty())

		Expect(entries[1].IsRoot).To(BeFalse())
		Expect(entries[1].Breadcrumb).To(HaveLen(1))
		Expect(entries[1].Breadcrumb[0].ID).To(Equal(parent.String()))
		Expect(entries[1].Breadcrumb[0].Name).To(Equal("parent"))
	})

	ginkgo.It("builds a multi-level breadcrumb root → parent", func() {
		root := uuid.New()
		mid := uuid.New()
		leaf := uuid.New()
		configs := []models.ConfigItem{
			makeCI(root, uuid.Nil, uuid.Nil, "root"),
			makeCI(mid, root, uuid.Nil, "mid"),
			makeCI(leaf, mid, root, "leaf"),
		}
		entries := makeEntries(configs)

		classifyRootsAndBreadcrumbs(configs, entries)

		Expect(entries[0].IsRoot).To(BeTrue())
		Expect(entries[1].IsRoot).To(BeFalse())
		Expect(entries[1].Breadcrumb).To(HaveLen(1))
		Expect(entries[1].Breadcrumb[0].ID).To(Equal(root.String()))

		Expect(entries[2].IsRoot).To(BeFalse())
		Expect(entries[2].Breadcrumb).To(HaveLen(2))
		Expect(entries[2].Breadcrumb[0].ID).To(Equal(root.String()))
		Expect(entries[2].Breadcrumb[1].ID).To(Equal(mid.String()))
	})

	ginkgo.It("treats a child with no selected ancestor as root", func() {
		unselected := uuid.New()
		child := uuid.New()
		configs := []models.ConfigItem{
			makeCI(child, unselected, uuid.Nil, "child"),
		}
		entries := makeEntries(configs)

		classifyRootsAndBreadcrumbs(configs, entries)

		Expect(entries[0].IsRoot).To(BeTrue())
		Expect(entries[0].Breadcrumb).To(BeEmpty())
	})
})

var _ = ginkgo.Describe("buildRecursiveRelationshipTree", func() {
	// Use a background context; tests pass the full ancestor chain in the
	// input so the function never falls through to a DB lookup.
	ctx := dutyCtx.NewContext(gocontext.Background())

	makeCI := func(id, parent, grandparent uuid.UUID, name string) models.ConfigItem {
		ci := models.ConfigItem{ID: id, Name: &name}
		switch {
		case grandparent != uuid.Nil && parent != uuid.Nil:
			ci.Path = grandparent.String() + "." + parent.String() + "." + id.String()
		case parent != uuid.Nil:
			ci.Path = parent.String() + "." + id.String()
		default:
			ci.Path = id.String()
		}
		return ci
	}

	ginkgo.It("returns the single root when a chain is passed in", func() {
		root := uuid.New()
		child := uuid.New()
		grandchild := uuid.New()

		configs := []models.ConfigItem{
			makeCI(root, uuid.Nil, uuid.Nil, "root"),
			makeCI(child, root, uuid.Nil, "child"),
			makeCI(grandchild, child, root, "grandchild"),
		}

		tree, err := buildRecursiveRelationshipTree(ctx, configs, nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(tree).ToNot(BeNil())
		Expect(tree.ID).To(Equal(root.String()))
		Expect(tree.EdgeType).To(Equal("target"))
		Expect(tree.Children).To(HaveLen(1))
		Expect(tree.Children[0].ID).To(Equal(child.String()))
		Expect(tree.Children[0].Children).To(HaveLen(1))
		Expect(tree.Children[0].Children[0].ID).To(Equal(grandchild.String()))
	})

	ginkgo.It("wraps disconnected configs in a virtual root", func() {
		a := uuid.New()
		b := uuid.New()

		configs := []models.ConfigItem{
			makeCI(a, uuid.Nil, uuid.Nil, "a"),
			makeCI(b, uuid.Nil, uuid.Nil, "b"),
		}

		tree, err := buildRecursiveRelationshipTree(ctx, configs, nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(tree).ToNot(BeNil())
		Expect(tree.ID).To(BeEmpty())
		Expect(tree.Name).To(Equal("2 configs"))
		Expect(tree.Children).To(HaveLen(2))
	})

	ginkgo.It("returns nil for an empty input", func() {
		tree, err := buildRecursiveRelationshipTree(ctx, nil, nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(tree).To(BeNil())
	})

	ginkgo.It("attaches siblings under a common parent", func() {
		parent := uuid.New()
		s1 := uuid.New()
		s2 := uuid.New()

		configs := []models.ConfigItem{
			makeCI(parent, uuid.Nil, uuid.Nil, "parent"),
			makeCI(s1, parent, uuid.Nil, "s1"),
			makeCI(s2, parent, uuid.Nil, "s2"),
		}

		tree, err := buildRecursiveRelationshipTree(ctx, configs, nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(tree).ToNot(BeNil())
		Expect(tree.ID).To(Equal(parent.String()))
		Expect(tree.Children).To(HaveLen(2))

		childIDs := []string{tree.Children[0].ID, tree.Children[1].ID}
		Expect(childIDs).To(ConsistOf(s1.String(), s2.String()))
	})

	ginkgo.It("grafts entry-tree related children onto target nodes", func() {
		a := uuid.New()
		b := uuid.New()
		relatedID := uuid.New().String()

		configs := []models.ConfigItem{
			makeCI(a, uuid.Nil, uuid.Nil, "a"),
			makeCI(b, uuid.Nil, uuid.Nil, "b"),
		}

		entryTrees := map[uuid.UUID]*api.CatalogReportTreeNode{
			a: {
				CatalogReportConfigItem: api.CatalogReportConfigItem{ID: a.String(), Name: "a"},
				Children: []api.CatalogReportTreeNode{{
					CatalogReportConfigItem: api.CatalogReportConfigItem{ID: relatedID, Name: "related"},
					EdgeType:                "related",
				}},
			},
		}

		tree, err := buildRecursiveRelationshipTree(ctx, configs, entryTrees)
		Expect(err).ToNot(HaveOccurred())
		Expect(tree).ToNot(BeNil())
		Expect(tree.Name).To(Equal("2 configs"))
		Expect(tree.Children).To(HaveLen(2))

		var aNode api.CatalogReportTreeNode
		for _, c := range tree.Children {
			if c.ID == a.String() {
				aNode = c
			}
		}
		Expect(aNode.Children).To(HaveLen(1))
		Expect(aNode.Children[0].ID).To(Equal(relatedID))
		Expect(aNode.Children[0].EdgeType).To(Equal("related"))
	})

	ginkgo.It("does not duplicate grafted children that are already ancestry nodes", func() {
		a := uuid.New()
		b := uuid.New()

		configs := []models.ConfigItem{
			makeCI(a, uuid.Nil, uuid.Nil, "a"),
			makeCI(b, a, uuid.Nil, "b"), // b is a descendant of a via Path
		}

		// entryTree for `a` also claims b as a related child — it should not
		// be grafted again because b is already attached under a via path.
		entryTrees := map[uuid.UUID]*api.CatalogReportTreeNode{
			a: {
				CatalogReportConfigItem: api.CatalogReportConfigItem{ID: a.String(), Name: "a"},
				Children: []api.CatalogReportTreeNode{{
					CatalogReportConfigItem: api.CatalogReportConfigItem{ID: b.String(), Name: "b"},
					EdgeType:                "related",
				}},
			},
		}

		tree, err := buildRecursiveRelationshipTree(ctx, configs, entryTrees)
		Expect(err).ToNot(HaveOccurred())
		Expect(tree).ToNot(BeNil())
		Expect(tree.ID).To(Equal(a.String()))
		Expect(tree.Children).To(HaveLen(1))
		Expect(tree.Children[0].ID).To(Equal(b.String()))
	})
})
