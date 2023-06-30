package upstream

import (
	"github.com/flanksource/duty/fixtures/dummy"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/incident-commander/api"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

var _ = ginkgo.Describe("Push Mode reconcilation", ginkgo.Ordered, func() {
	ginkgo.It("should populate the agent database with the 5 tables that are reconciled", func() {
		err := dummy.PopulateDBWithDummyModels(agentDB)
		Expect(err).To(BeNil())

		// TODO:
		// - Populate dummy config scraper (maybe add this in duty)
		// - Create a dummy component and config item that are linked to each other so the reconcilation fails
		// - Also test the hash is serving the purpose

		// Agent must have all of dummy records
		compareItemCounts[models.Component](agentDB, len(dummy.AllDummyComponents))
		compareItemCounts[models.ConfigItem](agentDB, len(dummy.AllDummyConfigs))
		compareItemCounts[models.Canary](agentDB, len(dummy.AllDummyCanaries))
		compareItemCounts[models.Check](agentDB, len(dummy.AllDummyChecks))

		// Upstream must have no records
		compareItemCounts[models.Component](upstreamDB, 0)
		compareItemCounts[models.ConfigItem](upstreamDB, 0)
		compareItemCounts[models.Canary](upstreamDB, 0)
		compareItemCounts[models.Check](upstreamDB, 0)
	})

	ginkgo.It("should run reconcilation", func() {
		ctx := api.NewContext(agentDB, nil)
		SyncWithUpstream(ctx)
	})

	ginkgo.It("should have transferred all the components", func() {
		var fieldsToIgnore []string
		fieldsToIgnore = append(fieldsToIgnore, "TopologyID")                                                    // Upstream creates its own dummy topology
		fieldsToIgnore = append(fieldsToIgnore, "Checks", "Components", "Order", "SelectorID", "RelationshipID") // These are auxiliary fields & do not represent the table columns.
		ignoreFieldsOpt := cmpopts.IgnoreFields(models.Component{}, fieldsToIgnore...)

		// unexported fields must be explicitly ignored.
		ignoreUnexportedOpt := cmpopts.IgnoreUnexported(models.Component{}, types.Summary{})

		compareEntities(upstreamDB, agentDB, &[]models.Component{}, ignoreFieldsOpt, ignoreUnexportedOpt)
	})

	ginkgo.It("should have transferred all the checks", func() {
		compareEntities(upstreamDB, agentDB, &[]models.Check{})
	})

	ginkgo.It("should have transferred all the canaries", func() {
		compareEntities(upstreamDB, agentDB, &[]models.Canary{})
	})

	ginkgo.It("should have transferred all the configs", func() {
		compareEntities(upstreamDB, agentDB, &[]models.ConfigItem{})
	})

	ginkgo.It("should have transferred all the config scrapers", func() {
		compareEntities(upstreamDB, agentDB, &[]models.ConfigScraper{})
	})
})

// compareEntities is a helper function that compares two sets of entities from an upstream and downstream database,
// ensuring that all records have been successfully transferred and match each other.
func compareEntities(upstreamDB, downstreamDB *gorm.DB, entityType interface{}, ignoreOpts ...cmp.Option) {
	var upstream, downstream []interface{}
	err := upstreamDB.Find(entityType).Error
	Expect(err).NotTo(HaveOccurred())

	err = downstreamDB.Find(entityType).Error
	Expect(err).NotTo(HaveOccurred())

	Expect(len(upstream)).To(Equal(len(downstream)))

	diff := cmp.Diff(upstream, downstream, ignoreOpts...)
	Expect(diff).To(BeEmpty())
}

// compareItemCounts compares whether the given model "T" has `totalExpected` number of records
// in the database
func compareItemCounts[T any](gormDB *gorm.DB, totalExpected int) {
	var result []T
	err := gormDB.Find(&result).Error
	Expect(err).To(BeNil())
	Expect(totalExpected).To(Equal(len(result)))
}
