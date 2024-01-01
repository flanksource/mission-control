package upstream

import (
	"fmt"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/duty/upstream"
	"github.com/flanksource/postq"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"

	"github.com/flanksource/incident-commander/api"
)

var _ = ginkgo.Describe("Upstream Push", ginkgo.Ordered, func() {
	// 1. Initial setup
	// 	a. Fire up a postgres server & create 2 databases for downstream & upstream servers with migrations run on both of them.
	// 	b. Fire up an HTTP Server for the upstream
	// 2. Insert new records for the monitored tables in downstream server & verify that those changes are reflected on the event_queue table.
	// 3. Update and delete some records and once again verify that those changes are reflected on the event_queue table.
	// 4. Setup event handler & provide upstream's configuration. This will transfer all the tables to upstream.
	// 5. Now, verify those records are available on the upstream's database.

	var (
		pushAgent    = agentWrapper{name: "push", id: uuid.New(), datasetFunc: dummy.GenerateDynamicDummyData}
		pushUpstream = agentWrapper{name: "push_upstream", id: uuid.New()}
	)

	ginkgo.BeforeAll(func() {
		pushAgent.setup(DefaultContext)
		pushUpstream.setup(DefaultContext)
		pushUpstream.StartServer()

		Expect(pushUpstream.DB().Create(&models.Agent{ID: pushAgent.id, Name: pushAgent.name}).Error).To(BeNil())

	})
	ginkgo.It("should track insertion on the event_queue table", func() {
		var events api.Events
		err := pushAgent.DB().Where("name = ?", api.EventPushQueueCreate).Find(&events).Error
		Expect(err).NotTo(HaveOccurred())
		verifyPushQueue(events.ToPostQEvents(), pushAgent.dataset)
	})

	ginkgo.It("should track updates & deletes on the event_queue table", func() {
		start := time.Now()

		modifiedNewDummy := dummy.Logistics
		modifiedNewDummy.ID = uuid.New()

		err := pushAgent.DB().Create(&modifiedNewDummy).Error
		Expect(err).NotTo(HaveOccurred())

		modifiedNewDummy.Status = types.ComponentStatusUnhealthy
		err = pushAgent.DB().Save(&modifiedNewDummy).Error
		Expect(err).NotTo(HaveOccurred())

		modifiedNewDummy.Status = types.ComponentStatusUnhealthy
		err = pushAgent.DB().Delete(&modifiedNewDummy).Error
		Expect(err).NotTo(HaveOccurred())

		var events api.Events
		err = pushAgent.DB().Where("name = ? AND created_at >= ?", api.EventPushQueueCreate, start).Find(&events).Error
		Expect(err).NotTo(HaveOccurred())

		// Only 1 event should get created since we are modifying the same resource
		Expect(len(events)).To(Equal(1))

		groupedEvents := upstream.GroupChangelogsByTables(events.ToPostQEvents())
		Expect(groupedEvents[0].ItemIDs).To(Equal([][]string{{modifiedNewDummy.ID.String()}}))
	})

	ginkgo.It("should process events", func() {
		pushAgent.PushTo(pushUpstream)
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

		ginkgo.It("should have transferred all the config analyses", func() {
			compareAgentEntities[models.ConfigAnalysis]("config_analysis", pushUpstream.DB(), pushAgent, cmpopts.IgnoreFields(models.ConfigAnalysis{}, "FirstObserved"))
		})

		ginkgo.It("should have transferred all the config changes", func() {
			compareAgentEntities[models.ConfigChange]("config_changes", pushUpstream.DB(), pushAgent)
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

// getPrimaryKeys extracts and returns the list of primary keys for the given table from the provided rows.
func getPrimaryKeys(table string, rows any) [][]string {
	var primaryKeys [][]string

	switch table {
	case "topologies":
		topologies := rows.([]models.Topology)
		for _, t := range topologies {
			primaryKeys = append(primaryKeys, []string{t.ID.String()})
		}

	case "canaries":
		canaries := rows.([]models.Canary)
		for _, c := range canaries {
			primaryKeys = append(primaryKeys, []string{c.ID.String()})
		}

	case "checks":
		checks := rows.([]models.Check)
		for _, c := range checks {
			primaryKeys = append(primaryKeys, []string{c.ID.String()})
		}

	case "components":
		components := rows.([]models.Component)
		for _, c := range components {
			primaryKeys = append(primaryKeys, []string{c.ID.String()})
		}

	case "config_scrapers":
		configScrapers := rows.([]models.ConfigScraper)
		for _, c := range configScrapers {
			primaryKeys = append(primaryKeys, []string{c.ID.String()})
		}

	case "config_items":
		configs := rows.([]models.ConfigItem)
		for _, c := range configs {
			primaryKeys = append(primaryKeys, []string{c.ID.String()})
		}

	case "config_changes":
		configChanges := rows.([]models.ConfigChange)
		for _, c := range configChanges {
			primaryKeys = append(primaryKeys, []string{c.ID})
		}

	case "config_analysis":
		configAnalyses := rows.([]models.ConfigAnalysis)
		for _, c := range configAnalyses {
			primaryKeys = append(primaryKeys, []string{c.ID.String()})
		}

	case "check_statuses":
		checkStatuses := rows.([]models.CheckStatus)
		for _, c := range checkStatuses {
			t, err := c.GetTime()
			if err != nil {
				logger.Errorf("failed to get check time[%s]: %v", c.Time, err)
				return nil
			}

			// The check statuses fixtures & .GetTime() method do not include timezone information.
			// Postgres stores the time in the local timezone.
			// We could modify the fixture & struct on duty or do it this way.
			t = replaceTimezone(t, time.Now().Local().Location())
			primaryKeys = append(primaryKeys, []string{c.CheckID.String(), t.Format("2006-01-02T15:04:05-07:00")})
		}

	case "component_relationships":
		componentRelationships := rows.([]models.ComponentRelationship)
		for _, c := range componentRelationships {
			primaryKeys = append(primaryKeys, []string{c.ComponentID.String(), c.RelationshipID.String(), c.SelectorID})
		}

	case "config_component_relationships":
		configComponentRelationships := rows.([]models.ConfigComponentRelationship)
		for _, c := range configComponentRelationships {
			primaryKeys = append(primaryKeys, []string{c.ComponentID.String(), c.ConfigID.String()})
		}

	default:
		return nil
	}

	return primaryKeys
}

// replaceTimezone creates a new time.Time from the given time.Time with the provided location.
// Timezone conversion is not performed.
func replaceTimezone(t time.Time, newLocation *time.Location) time.Time {
	year, month, day := t.Date()
	hour, minute, second := t.Clock()
	nano := t.Nanosecond()

	return time.Date(year, month, day, hour, minute, second, nano, newLocation)
}

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

	case "config_analysis":
		err = upstreamDB.Joins("LEFT JOIN config_items ON config_items.id = config_analysis.config_id").Where("config_items.agent_id = ?", agent.id).Order("id").Find(&upstream).Error
		agentErr = agent.DB().Order("id").Find(&downstream).Error

	case "config_changes":
		err = upstreamDB.Joins("LEFT JOIN config_items ON config_items.id = config_changes.config_id").Where("config_items.agent_id = ?", agent.id).Order("created_at").Find(&upstream).Error
		agentErr = agent.DB().Order("created_at").Find(&downstream).Error

	case "component_relationships":
		err = upstreamDB.Joins("LEFT JOIN components c1 ON component_relationships.component_id = c1.id").
			Joins("LEFT JOIN components c2 ON component_relationships.relationship_id = c2.id").
			Where("c1.agent_id = ? OR c2.agent_id = ?", agent.id, agent.id).Order("created_at").Find(&upstream).Error
		agentErr = agent.DB().Order("created_at").Find(&downstream).Error

	case "config_component_relationships":
		err = upstreamDB.Joins("LEFT JOIN components ON config_component_relationships.component_id = components.id").
			Joins("LEFT JOIN config_items ON config_items.id = config_component_relationships.config_id").
			Where("components.agent_id = ? OR config_items.agent_id = ?", agent.id, agent.id).Order("created_at").Find(&upstream).Error
		agentErr = agent.DB().Order("created_at").Find(&downstream).Error

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

func verifyPushQueue(events postq.Events, dataset dummy.DummyData) {
	groupedEvents := upstream.GroupChangelogsByTables(events)
	for _, g := range groupedEvents {
		table := g.TableName
		switch table {
		case "canaries":
			Expect(len(g.ItemIDs)).To(Equal(len(dataset.Canaries)))
			Expect(g.ItemIDs).To(Equal(getPrimaryKeys(table, dataset.Canaries)), "Mismatch primary keys for canaries")

		case "topologies":
			Expect(len(g.ItemIDs)).To(Equal(len(dataset.Topologies)))
			Expect(g.ItemIDs).To(Equal(getPrimaryKeys(table, dataset.Topologies)), "Mismatch primary keys for topologies")

		case "checks":
			Expect(len(g.ItemIDs)).To(Equal(len(dataset.Checks)))
			Expect(g.ItemIDs).To(Equal(getPrimaryKeys(table, dataset.Checks)), "Mismatch primary keys for checks")

		case "components":
			Expect(len(g.ItemIDs)).To(Equal(len(dataset.Components)))
			Expect(g.ItemIDs).To(Equal(getPrimaryKeys(table, dataset.Components)), "Mismatch primary keys for components")

		case "config_scrapers":
			Expect(len(g.ItemIDs)).To(Equal(len(dataset.ConfigScrapers)))
			Expect(g.ItemIDs).To(Equal(getPrimaryKeys(table, dataset.ConfigScrapers)), "Mismatch primary keys for config_scrapers")

		case "config_items":
			Expect(len(g.ItemIDs)).To(Equal(len(dataset.Configs)))
			Expect(g.ItemIDs).To(Equal(getPrimaryKeys(table, dataset.Configs)), "Mismatch primary keys for config_items")

		case "config_analysis":
			Expect(len(g.ItemIDs)).To(Equal(len(dataset.ConfigAnalyses)))
			Expect(g.ItemIDs).To(Equal(getPrimaryKeys(table, dataset.ConfigAnalyses)), "Mismatch primary keys for config_analysis")

		case "check_statuses":
			Expect(len(g.ItemIDs)).To(Equal(len(dataset.CheckStatuses)))
			Expect(g.ItemIDs).To(Equal(getPrimaryKeys(table, dataset.CheckStatuses)), "Mismatch composite primary keys for check_statuses")

		case "component_relationships":
			Expect(len(g.ItemIDs)).To(Equal(len(dataset.ComponentRelationships)))
			Expect(g.ItemIDs).To(Equal(getPrimaryKeys(table, dataset.ComponentRelationships)), "Mismatch composite primary keys for component_relationships")

		case "config_component_relationships":
			Expect(len(g.ItemIDs)).To(Equal(len(dataset.ConfigComponentRelationships)))
			Expect(g.ItemIDs).To(Equal(getPrimaryKeys(table, dataset.ConfigComponentRelationships)), "Mismatch composite primary keys for config_component_relationships")

		case "config_changes":
			Expect(len(g.ItemIDs)).To(Equal(len(dataset.ConfigChanges)))
			Expect(g.ItemIDs).To(Equal(getPrimaryKeys(table, dataset.ConfigChanges)), "Mismatch composite primary keys for config_changes")

		case "config_relationships":
			// Do nothing (need to populate the config_relationships table)

		default:
			ginkgo.Fail(fmt.Sprintf("Unexpected table %q on the event queue for %q", table, api.EventPushQueueCreate))
		}
	}

	Expect(len(events)).To(Equal(
		len(dataset.Canaries) +
			len(dataset.Topologies) +
			len(dataset.Checks) +
			len(dataset.Components) +
			len(dataset.ConfigScrapers) +
			len(dataset.Configs) +
			len(dataset.ConfigChanges) +
			len(dataset.ConfigAnalyses) +
			len(dataset.ComponentRelationships) +
			len(dataset.ConfigComponentRelationships)))
}
