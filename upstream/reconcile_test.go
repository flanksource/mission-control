package upstream

import (
	gocontext "context"
	"fmt"
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
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

var _ = ginkgo.Describe("Push Mode reconcilation", ginkgo.Ordered, func() {
	const randomConfigItemCount = 2000 // keep it below 5k (must be set w.r.t the page size)

	ginkgo.It("should populate the agent database with the 6 tables that are reconciled", func() {
		err := dummyDataset.Populate(agentDB)
		Expect(err).To(BeNil())

		// duty's dummy fixture doesn't have a dummy config scraper (maybe add this in duty)
		dummyConfigScraper := models.ConfigScraper{
			ID:        uuid.New(),
			Name:      "Azure scraper",
			CreatedAt: time.Now(),
			Source:    "ConfigFile",
			Spec:      "{}",
		}
		Expect(agentDB.Create(&dummyConfigScraper).Error).To(BeNil(), "save config scraper")

		// Agent must have all of dummy records
		compareItemsCount[models.Component](agentDB, len(dummyDataset.Components), "agent-Component")
		compareItemsCount[models.ConfigItem](agentDB, len(dummyDataset.Configs), "agent-ConfigItem")
		compareItemsCount[models.ConfigScraper](agentDB, 1, "agent-ConfigScraper")
		compareItemsCount[models.Canary](agentDB, len(dummyDataset.Canaries), "agent-Canary")
		compareItemsCount[models.Check](agentDB, len(dummyDataset.Checks), "agent-Check")
		compareItemsCount[models.CheckStatus](agentDB, len(dummyDataset.CheckStatuses), "agent-CheckStatus")

		// Upstream must have no records
		compareItemsCount[models.Component](upstreamDB, 0, "upstream-Component")
		compareItemsCount[models.ConfigItem](upstreamDB, 0, "upstream-ConfigItem")
		compareItemsCount[models.ConfigScraper](upstreamDB, 0, "upstream-ConfigScraper")
		compareItemsCount[models.Canary](upstreamDB, 0, "upstream-Canary")
		compareItemsCount[models.Check](upstreamDB, 0, "upstream-Check")
		compareItemsCount[models.CheckStatus](upstreamDB, 0, "upstream-CheckStatus")
	})

	ginkgo.It("should return different hash for agent and upstream", func() {
		ctx := context.NewContext(gocontext.Background()).WithDB(agentDB, agentDBPGPool)
		upstreamCtx := context.NewContext(gocontext.Background()).WithDB(upstreamDB, upstreamPool)

		for _, table := range api.TablesToReconcile {
			paginateRequest := upstream.PaginateRequest{From: "", Table: table, Size: 500}
			if table == "check_statuses" {
				paginateRequest.From = ","
			}

			agentStatus, err := upstream.GetPrimaryKeysHash(ctx, paginateRequest, uuid.Nil)
			Expect(err).To(BeNil())

			upstreamStatus, err := upstream.GetPrimaryKeysHash(upstreamCtx, paginateRequest, uuid.Nil)
			Expect(err).To(BeNil())

			Expect(agentStatus).ToNot(Equal(upstreamStatus), fmt.Sprintf("table [%s] hash to not match", table))
		}
	})

	ginkgo.It("should reconcile all the tables", func() {
		ctx := context.NewContext(gocontext.Background()).WithDB(agentDB, agentDBPGPool)

		reconciler := upstream.NewUpstreamReconciler(api.UpstreamConf, 500)
		for _, table := range api.TablesToReconcile {
			err := reconciler.Sync(ctx, table)
			Expect(err).To(BeNil(), fmt.Sprintf("should push table '%s' to upstream", table))
		}
	})

	ginkgo.It("should match the hash", func() {
		ctx := context.NewContext(gocontext.Background()).WithDB(agentDB, agentDBPGPool)
		upstreamCtx := context.NewContext(gocontext.Background()).WithDB(upstreamDB, upstreamPool)

		for _, table := range api.TablesToReconcile {
			paginateRequest := upstream.PaginateRequest{From: "", Table: table, Size: 500}
			if table == "check_statuses" {
				paginateRequest.From = ","
			}

			agentStatus, err := upstream.GetPrimaryKeysHash(ctx, paginateRequest, uuid.Nil)
			Expect(err).To(BeNil())

			upstreamStatus, err := upstream.GetPrimaryKeysHash(upstreamCtx, paginateRequest, agentID)
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

		compareEntities(upstreamDB, agentDB, &[]models.Component{}, ignoreFieldsOpt, ignoreUnexportedOpt)
	})

	ginkgo.It("should have transferred all the checks", func() {
		compareEntities(upstreamDB, agentDB, &[]models.Check{})
	})

	ginkgo.It("should have transferred all the check statuses", func() {
		compareEntities(upstreamDB, agentDB, &[]models.CheckStatus{})
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

	ginkgo.It(fmt.Sprintf("should generated %d dummy config items and save on agent", randomConfigItemCount), func() {
		dummyConfigItems := make([]models.ConfigItem, randomConfigItemCount)
		for i := 0; i < randomConfigItemCount; i++ {
			dummyConfigItems[i] = models.ConfigItem{
				ID:          uuid.New(),
				ConfigClass: models.ConfigClassCluster,
			}
		}

		Expect(agentDB.CreateInBatches(&dummyConfigItems, 2000).Error).To(BeNil())
	})

	ginkgo.It("should reconcile config items", func() {
		ctx := context.NewContext(gocontext.Background()).WithDB(agentDB, agentDBPGPool)

		reconciler := upstream.NewUpstreamReconciler(api.UpstreamConf, 500)
		err := reconciler.Sync(ctx, "config_items")
		Expect(err).To(BeNil(), "should push table 'config_items' upstream")
	})

	ginkgo.It("should have transferred all the new config items", func() {
		compareEntities(upstreamDB, agentDB, &[]models.ConfigItem{})
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

// compareItemsCount compares whether the given model "T" has `totalExpected` number of records
// in the database
func compareItemsCount[T any](gormDB *gorm.DB, totalExpected int, description ...any) {
	var result []T
	err := gormDB.Find(&result).Error
	Expect(err).To(BeNil(), description...)
	Expect(totalExpected).To(Equal(len(result)))
}
