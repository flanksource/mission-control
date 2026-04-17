package catalog

import (
	"testing"
	"time"

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

		result := configTreeNodeToReport(nodeA)
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

		result := configTreeNodeToReport(nodeA)
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

		result := configTreeNodeToReport(nodeA)
		Expect(result.Children).To(HaveLen(1))
		Expect(result.Children[0].Children).To(HaveLen(1))
		Expect(result.Children[0].Children[0].Children).To(BeEmpty())
	})
})
