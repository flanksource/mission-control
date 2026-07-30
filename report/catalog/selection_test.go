package catalog

import (
	gocontext "context"

	commons "github.com/flanksource/commons/context"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("Catalog report selection", func() {
	ctx := context.Context{Context: commons.NewContext(gocontext.TODO())}

	ginkgo.It("deduplicates explicit roots without recursive database queries", func() {
		name := "root"
		root := models.ConfigItem{ID: uuid.New(), Name: &name}

		selection, err := ResolveSelection(ctx, []models.ConfigItem{root, root}, false, "")
		Expect(err).NotTo(HaveOccurred())
		Expect(selection.Roots).To(HaveLen(1))
		Expect(selection.Items).To(HaveLen(1))
		Expect(selection.ItemsByRoot[root.ID]).To(Equal([]models.ConfigItem{root}))
	})

	ginkgo.It("assigns descendants to the nearest explicitly selected root", func() {
		outerID := uuid.New()
		innerID := uuid.New()
		name := "child"
		child := models.ConfigItem{
			ID:   uuid.New(),
			Name: &name,
			Path: outerID.String() + "." + innerID.String(),
		}

		owner := owningRoot(child, map[uuid.UUID]bool{outerID: true, innerID: true})
		Expect(owner).To(Equal(innerID))
	})
})
