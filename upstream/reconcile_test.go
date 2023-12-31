package upstream

import (
	"fmt"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/duty/upstream"
	"github.com/flanksource/incident-commander/api"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

var _ = ginkgo.Describe("Upstream Reconcile", ginkgo.Ordered, func() {
	var reconcileAgent = agentWrapper{name: "reconcile", id: uuid.New(), datasetFunc: dummy.GenerateDynamicDummyData}
	var reconcileUpstream = agentWrapper{name: "reconcile_upstream", id: uuid.New()}

	ginkgo.BeforeAll(func() {
		reconcileAgent.setup(DefaultContext)
		reconcileUpstream.setup(DefaultContext)
		reconcileUpstream.StartServer()

		Expect(reconcileUpstream.DB().Create(&models.Agent{ID: reconcileAgent.id, Name: reconcileAgent.name}).Error).To(BeNil())

	})
	ginkgo.It("should populate the agent database with the 6 tables that are reconciled", func() {
		dummyDataset := reconcileAgent.dataset

		// Agent must have all of dummy records
		compareItemsCount[models.Topology](reconcileAgent.DB(), len(dummyDataset.Topologies), "agent-Topology")
		compareItemsCount[models.Component](reconcileAgent.DB(), len(dummyDataset.Components), "agent-Component")
		compareItemsCount[models.ConfigItem](reconcileAgent.DB(), len(dummyDataset.Configs), "agent-ConfigItem")
		compareItemsCount[models.ConfigScraper](reconcileAgent.DB(), len(dummyDataset.ConfigScrapers), "agent-ConfigScraper")
		compareItemsCount[models.Canary](reconcileAgent.DB(), len(dummyDataset.Canaries), "agent-Canary")
		compareItemsCount[models.Check](reconcileAgent.DB(), len(dummyDataset.Checks), "agent-Check")
		compareItemsCount[models.CheckStatus](reconcileAgent.DB(), len(dummyDataset.CheckStatuses), "agent-CheckStatus")

		// Upstream must have no records
		compareItemsCount[models.Topology](reconcileUpstream.DB(), 0, "upstream-Topology")
		compareItemsCount[models.Component](reconcileUpstream.DB(), 0, "upstream-Component")
		compareItemsCount[models.ConfigItem](reconcileUpstream.DB(), 0, "upstream-ConfigItem")
		compareItemsCount[models.ConfigScraper](reconcileUpstream.DB(), 0, "upstream-ConfigScraper")
		compareItemsCount[models.Canary](reconcileUpstream.DB(), 0, "upstream-Canary")
		compareItemsCount[models.Check](reconcileUpstream.DB(), 0, "upstream-Check")
		compareItemsCount[models.CheckStatus](reconcileUpstream.DB(), 0, "upstream-CheckStatus")

	})

	ginkgo.It("should return different hash for agent and upstream", func() {
		for _, table := range api.TablesToReconcile {
			paginateRequest := upstream.PaginateRequest{From: "", Table: table, Size: 500}
			if table == "check_statuses" {
				paginateRequest.From = ","
			}

			agentStatus, err := upstream.GetPrimaryKeysHash(reconcileAgent.Context, paginateRequest, uuid.Nil)
			Expect(err).To(BeNil())

			upstreamStatus, err := upstream.GetPrimaryKeysHash(reconcileUpstream.Context, paginateRequest, uuid.Nil)
			Expect(err).To(BeNil())

			Expect(agentStatus).ToNot(Equal(upstreamStatus), fmt.Sprintf("table [%s] hash to not match", table))
		}
	})

	ginkgo.It("should reconcile all the tables", func() {
		reconciler := reconcileAgent.GetReconciler(&reconcileUpstream)
		for _, table := range api.TablesToReconcile {
			count, err := reconciler.Sync(reconcileAgent.Context, table)
			Expect(err).To(BeNil(), fmt.Sprintf("should push table '%s' to upstream", table))
			Expect(count).To(BeNumerically(">", 0), fmt.Sprintf("should push more than 0 records '%s' to upstream", table))
		}
	})

	ginkgo.It("should match the hash", func() {
		for _, table := range api.TablesToReconcile {
			paginateRequest := upstream.PaginateRequest{From: "", Table: table, Size: 500}
			if table == "check_statuses" {
				paginateRequest.From = ","
			}

			agentStatus, err := upstream.GetPrimaryKeysHash(reconcileAgent.Context, paginateRequest, uuid.Nil)
			Expect(err).To(BeNil())

			upstreamStatus, err := upstream.GetPrimaryKeysHash(reconcileUpstream.Context, paginateRequest, reconcileAgent.id)
			Expect(err).To(BeNil())

			Expect(agentStatus).To(Equal(upstreamStatus), fmt.Sprintf("table [%s] hash to match", table))
		}
	})

	ginkgo.It("should have transferred all the components", func() {
		var fieldsToIgnore []string
		fieldsToIgnore = append(fieldsToIgnore, "TopologyID")                                                    // Upstream creates its own dummy topology
		fieldsToIgnore = append(fieldsToIgnore, "Checks", "Components", "Order", "SelectorID", "RelationshipID") // These are auxiliary fields & do not represent the table columns.
		ignoreFieldsOpt := cmpopts.IgnoreFields(models.Component{}, fieldsToIgnore...)

		// unexported fields must be explicitly ignored.
		ignoreUnexportedOpt := cmpopts.IgnoreUnexported(models.Component{}, types.Summary{})

		compareEntities(reconcileUpstream.DB(), reconcileAgent.DB(), &[]models.Component{}, ignoreFieldsOpt, ignoreUnexportedOpt)
	})

	ginkgo.It("should have transferred all the checks", func() {
		compareEntities(reconcileUpstream.DB(), reconcileAgent.DB(), &[]models.Check{})
	})

	ginkgo.It("should have transferred all the check statuses", func() {
		compareEntities(reconcileUpstream.DB(), reconcileAgent.DB(), &[]models.CheckStatus{})
	})

	ginkgo.It("should have transferred all the canaries", func() {
		compareEntities(reconcileUpstream.DB(), reconcileAgent.DB(), &[]models.Canary{})
	})

	ginkgo.It("should have transferred all the configs", func() {
		compareEntities(reconcileUpstream.DB(), reconcileAgent.DB(), &[]models.ConfigItem{})
	})

	ginkgo.It("should have transferred all the config scrapers", func() {
		compareEntities(reconcileUpstream.DB(), reconcileAgent.DB(), &[]models.ConfigScraper{})
	})

	ginkgo.FIt(fmt.Sprintf("should generated %d dummy config items and save on agent", batchSize), func() {
		var dummyConfigItems []models.ConfigItem
		for i := 0; i < batchSize*4+batchSize/2; i++ {
			dummyConfigItems = append(dummyConfigItems, models.ConfigItem{
				ID:          uuid.New(),
				ConfigClass: models.ConfigClassCluster,
			})
		}

		Expect(reconcileAgent.DB().CreateInBatches(&dummyConfigItems, 500).Error).To(BeNil())
	})

	ginkgo.FIt("should reconcile config items", func() {
		count, err := reconcileAgent.Reconcile(&reconcileUpstream, "config_items")
		Expect(err).To(BeNil(), "should push table 'config_items' upstream")
		Expect(count).To(BeNumerically(">", 1))
		count, err = reconcileAgent.Reconcile(&reconcileUpstream, "config_items")
		Expect(err).To(BeNil(), "should push table 'config_items' upstream")
		Expect(count).To(BeNumerically("==", 0))

	})

	ginkgo.It("should have transferred all the new config items", func() {
		compareEntities(reconcileUpstream.DB(), reconcileAgent.DB(), &[]models.ConfigItem{})
	})
})

// compareEntities is a helper function that compares two sets of entities from an upstream and downstream database,
// ensuring that all records have been successfully transferred and match each other.
func compareEntities(upstreamDB *gorm.DB, downstreamDB *gorm.DB, entityType interface{}, ignoreOpts ...cmp.Option) {
	var upstream, downstream []interface{}
	err := upstreamDB.Find(entityType).Error
	Expect(err).NotTo(HaveOccurred())

	err = downstreamDB.Find(entityType).Error
	Expect(err).NotTo(HaveOccurred())

	Expect(len(upstream)).To(Equal(len(downstream)))

	diff := cmp.Diff(upstream, downstream, ignoreOpts...)
	Expect(diff).To(BeEmpty())
}

// compareItemsCount compares whether the given model "T" has `totalExpected` number of records
// in the database
func compareItemsCount[T any](gormDB *gorm.DB, totalExpected int, description ...any) {
	var result []T
	err := gormDB.Find(&result).Error
	Expect(err).To(BeNil(), description...)
	Expect(totalExpected).To(BeNumerically(">=", len(result)))
}
