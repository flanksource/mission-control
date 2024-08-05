package upstream

import (
	"fmt"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/flanksource/duty/types"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

var _ = ginkgo.Describe("Upstream Push", ginkgo.Ordered, func() {
	var (
		pushAgent    = agentWrapper{name: "push", id: uuid.New(), datasetFunc: dummy.GenerateDynamicDummyData}
		pushUpstream = agentWrapper{name: "push_upstream", id: uuid.New()}
	)

	ginkgo.BeforeAll(func() {
		pushAgent.setup(DefaultContext)
		pushUpstream.setup(DefaultContext)
		pushUpstream.StartServer()

		DefaultContext.ClearCache()
		context.SetLocalProperty("upstream.reconcile.pre-check", "false")

		Expect(pushUpstream.DB().Create(&models.Agent{ID: pushAgent.id, Name: pushAgent.name}).Error).To(BeNil())
	})

	ginkgo.It("should push all tables", func() {
		err := pushAgent.Reconcile(pushUpstream.port)
		Expect(err).ToNot(HaveOccurred())
	})

	ginkgo.Describe("consumer", func() {
		ginkgo.It("should have transferred all the components", func() {
			var fieldsToIgnore []string
			fieldsToIgnore = append(fieldsToIgnore, "TopologyID")                                                    // Upstream creates its own dummy topology
			fieldsToIgnore = append(fieldsToIgnore, "Checks", "Components", "Order", "SelectorID", "RelationshipID") // These are auxiliary fields & do not represent the table columns.
			ignoreFieldsOpt := cmpopts.IgnoreFields(models.Component{}, fieldsToIgnore...)

			// unexported fields must be explicitly ignored.
			ignoreUnexportedOpt := cmpopts.IgnoreUnexported(models.Component{}, types.Summary{})

			compareAgentEntities[models.Component]("", pushUpstream.DB(), pushAgent, ignoreFieldsOpt, ignoreUnexportedOpt, cmpopts.IgnoreFields(models.Component{}, "AgentID"))
		})

		ginkgo.It("should have transferred all the checks", func() {
			compareAgentEntities[models.Check]("", pushUpstream.DB(), pushAgent, cmpopts.IgnoreFields(models.Check{}, "AgentID"))
		})

		ginkgo.It("should have transferred all the check statuses", func() {
			ginkgo.Skip("Skipping. Check statuses are not synced to upstream yet because of foreign key issues.")

			compareAgentEntities[models.CheckStatus]("check_statuses", pushUpstream.DB(), pushAgent)
		})

		ginkgo.It("should have transferred all the canaries", func() {
			compareAgentEntities[models.Canary]("", pushUpstream.DB(), pushAgent, cmpopts.IgnoreFields(models.Canary{}, "AgentID"))
		})

		ginkgo.It("should have transferred all the configs", func() {
			compareAgentEntities[models.ConfigItem]("", pushUpstream.DB(), pushAgent, cmpopts.IgnoreFields(models.ConfigItem{}, "AgentID"))
		})

		ginkgo.It("should have transferred all the config relationships", func() {
			compareAgentEntities[models.ComponentRelationship]("component_relationships", pushUpstream.DB(), pushAgent)
		})

		ginkgo.It("should have transferred all the config component relationships", func() {
			compareAgentEntities[models.ConfigComponentRelationship]("config_component_relationships", pushUpstream.DB(), pushAgent)
		})

		ginkgo.It("should have populated the agent_id field", func() {
			var count int
			err := pushUpstream.DB().Select("COUNT(*)").Table("checks").Where("agent_id IS NOT NULL").Scan(&count).Error
			Expect(err).ToNot(HaveOccurred())
			Expect(count).ToNot(BeZero())
		})
	})
})

// compareAgentEntities is a helper function that compares two sets of entities from an upstream and downstream database,
// ensuring that all records have been successfully transferred and match each other.
func compareAgentEntities[T any](table string, upstreamDB *gorm.DB, agent agentWrapper, ignoreOpts ...cmp.Option) {
	var upstream, downstream []T
	var err error
	var agentErr error

	// We're conditionally fetching the records from upstream and agent db
	// because we need to ensure
	// - that upstream only fetches the items for the given agent
	// - and the order of the items when fetching from upstream and agent db is identitcal for the comparison to work
	switch table {
	case "check_statuses":
		err = upstreamDB.Joins("LEFT JOIN checks ON checks.id = check_statuses.check_id").Where("checks.agent_id = ?", agent.id).Order("check_id, time").Find(&upstream).Error
		agentErr = agent.DB().Order("check_id, time").Find(&downstream).Error

	case "component_relationships":
		err = upstreamDB.Joins("LEFT JOIN components c1 ON component_relationships.component_id = c1.id").
			Joins("LEFT JOIN components c2 ON component_relationships.relationship_id = c2.id").
			Where("c1.agent_id = ? OR c2.agent_id = ?", agent.id, agent.id).Order("created_at").Find(&upstream).Error
		agentErr = agent.DB().Order("created_at").Find(&downstream).Error

	case "config_component_relationships":
		err = upstreamDB.Joins("LEFT JOIN components ON config_component_relationships.component_id = components.id").
			Joins("LEFT JOIN config_items ON config_items.id = config_component_relationships.config_id").
			Where("components.agent_id = ? OR config_items.agent_id = ?", agent.id, agent.id).Order("component_id, config_id, created_at").Find(&upstream).Error
		agentErr = agent.DB().Order("component_id, config_id, created_at").Find(&downstream).Error

	default:
		err = upstreamDB.Where("agent_id = ?", agent.id).Order("id").Find(&upstream).Error
		agentErr = agent.DB().Order("id").Find(&downstream).Error
	}

	Expect(err).NotTo(HaveOccurred())
	Expect(agentErr).NotTo(HaveOccurred())

	Expect(len(upstream)).To(Equal(len(downstream)), fmt.Sprintf("expected %s to sync all items to upstream ", agent.name))

	diff := cmp.Diff(upstream, downstream, ignoreOpts...)
	Expect(diff).To(BeEmpty(), fmt.Sprintf("expected %s to sync correct items to upstream ", agent.name))
}
