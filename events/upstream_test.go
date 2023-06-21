package events

import (
	"context"
	"fmt"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/fixtures/dummy"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/testutils"
)

var _ = ginkgo.Describe("Push Mode", ginkgo.Ordered, func() {
	// 1. Initial setup
	// 	a. Fire up a postgres server & create 2 databases for downstream & upstream servers with migrations run on both of them.
	// 	b. Fire up an HTTP Server for the upstream
	// 2. Insert new records for the monitored tables in downstream server & verify that those changes are reflected on the event_queue table.
	// 3. Update and delete some records and once again verify that those changes are reflected on the event_queue table.
	// 4. Setup event handler & provide upstream's configuration. This will transfer all the tables to upstream.
	// 5. Now, verify those records are available on the upstream's database.

	ginkgo.It("should track insertion on the event_queue table", func() {
		var events []api.Event
		err := testutils.TestDB.Where("name = ?", EventPushQueueCreate).Find(&events).Error
		Expect(err).NotTo(HaveOccurred())

		groupedEvents := GroupChangelogsByTables(events)
		for table, itemIDs := range groupedEvents {
			switch table {
			case "canaries":
				Expect(len(itemIDs)).To(Equal(len(dummy.AllDummyCanaries)))
				Expect(itemIDs).To(Equal(getPrimaryKeys(table, dummy.AllDummyCanaries)), "Mismatch primary keys for canaries")

			case "checks":
				Expect(len(itemIDs)).To(Equal(len(dummy.AllDummyChecks)))
				Expect(itemIDs).To(Equal(getPrimaryKeys(table, dummy.AllDummyChecks)), "Mismatch primary keys for checks")

			case "components":
				Expect(len(itemIDs)).To(Equal(len(dummy.AllDummyComponents)))
				Expect(itemIDs).To(Equal(getPrimaryKeys(table, dummy.AllDummyComponents)), "Mismatch primary keys for components")

			case "config_items":
				Expect(len(itemIDs)).To(Equal(len(dummy.AllDummyConfigs)))
				Expect(itemIDs).To(Equal(getPrimaryKeys(table, dummy.AllDummyConfigs)), "Mismatch primary keys for config_items")

			case "config_analysis":
				Expect(len(itemIDs)).To(Equal(len(dummy.AllDummyConfigAnalysis)))
				Expect(itemIDs).To(Equal(getPrimaryKeys(table, dummy.AllDummyConfigAnalysis)), "Mismatch primary keys for config_analysis")

			case "check_statuses":
				Expect(len(itemIDs)).To(Equal(len(dummy.AllDummyCheckStatuses)))
				Expect(itemIDs).To(Equal(getPrimaryKeys(table, dummy.AllDummyCheckStatuses)), "Mismatch composite primary keys for check_statuses")

			case "component_relationships":
				Expect(len(itemIDs)).To(Equal(len(dummy.AllDummyComponentRelationships)))
				Expect(itemIDs).To(Equal(getPrimaryKeys(table, dummy.AllDummyComponentRelationships)), "Mismatch composite primary keys for component_relationships")

			case "config_component_relationships":
				Expect(len(itemIDs)).To(Equal(len(dummy.AllDummyConfigComponentRelationships)))
				Expect(itemIDs).To(Equal(getPrimaryKeys(table, dummy.AllDummyConfigComponentRelationships)), "Mismatch composite primary keys for config_component_relationships")

			case "config_changes":
				Expect(len(itemIDs)).To(Equal(len(dummy.AllDummyConfigChanges)))
				Expect(itemIDs).To(Equal(getPrimaryKeys(table, dummy.AllDummyConfigChanges)), "Mismatch composite primary keys for config_changes")

			case "config_relationships":
				// Do nothing (need to populate the config_relationships table)

			default:
				ginkgo.Fail(fmt.Sprintf("Unexpected table %q on the event queue for %q", table, EventPushQueueCreate))
			}
		}

		Expect(len(events)).To(Equal(
			len(dummy.AllDummyCanaries) +
				len(dummy.AllDummyChecks) +
				len(dummy.AllDummyComponents) +
				len(dummy.AllDummyConfigs) +
				len(dummy.AllDummyConfigChanges) +
				len(dummy.AllDummyConfigAnalysis) +
				len(dummy.AllDummyCheckStatuses) +
				len(dummy.AllDummyComponentRelationships) +
				len(dummy.AllDummyConfigComponentRelationships)))
	})

	ginkgo.It("should track updates & deletes on the event_queue table", func() {
		start := time.Now()

		modifiedNewDummy := dummy.Logistics
		modifiedNewDummy.ID = uuid.New()

		err := testutils.TestDB.Create(&modifiedNewDummy).Error
		Expect(err).NotTo(HaveOccurred())

		modifiedNewDummy.Status = types.ComponentStatusUnhealthy
		err = testutils.TestDB.Save(&modifiedNewDummy).Error
		Expect(err).NotTo(HaveOccurred())

		modifiedNewDummy.Status = types.ComponentStatusUnhealthy
		err = testutils.TestDB.Delete(&modifiedNewDummy).Error
		Expect(err).NotTo(HaveOccurred())

		var events []api.Event
		err = testutils.TestDB.Where("name = ? AND created_at >= ?", EventPushQueueCreate, start).Find(&events).Error
		Expect(err).NotTo(HaveOccurred())

		// Only 1 event should get created since we are modifying the same resource
		Expect(len(events)).To(Equal(1))

		groupedEvents := GroupChangelogsByTables(events)
		Expect(groupedEvents["components"]).To(Equal([][]string{{modifiedNewDummy.ID.String()}}))
	})

	ginkgo.It("should transfer all events to upstream server", func() {
		// Overwrite the global db that'll be used by the upstream server.
		db.Gorm = testutils.TestUpstreamDB
		db.Pool = testutils.TestUpstreamDBPGPool

		eventHandlerConfig := Config{
			UpstreamConf: api.UpstreamConfig{
				AgentName: "test-agent",
				Host:      fmt.Sprintf("http://localhost:%d", testutils.TestUpstreamServerPort),
				Username:  "admin@local",
				Password:  "admin",
				Labels:    []string{"test"},
			},
		}

		eventHandler := NewEventHandler(context.Background(), testutils.TestDB, eventHandlerConfig)
		eventHandler.ConsumeEventsUntilEmpty()
	})

	ginkgo.It("should have transferred all the components", func() {
		var fieldsToIgnore []string
		fieldsToIgnore = append(fieldsToIgnore, "TopologyID")                                                    // Upstream creates its own dummy topology
		fieldsToIgnore = append(fieldsToIgnore, "Checks", "Components", "Order", "SelectorID", "RelationshipID") // These are auxiliary fields & do not represent the table columns.
		ignoreFieldsOpt := cmpopts.IgnoreFields(models.Component{}, fieldsToIgnore...)

		// unexported fields must be explicitly ignored.
		ignoreUnexportedOpt := cmpopts.IgnoreUnexported(models.Component{}, types.Summary{})

		compareEntities(testutils.TestUpstreamDB, testutils.TestDB, &[]models.Component{}, ignoreFieldsOpt, ignoreUnexportedOpt)
	})

	ginkgo.It("should have transferred all the checks", func() {
		compareEntities(testutils.TestUpstreamDB, testutils.TestUpstreamDB, &[]models.Check{})
	})

	ginkgo.It("should have transferred all the check statuses", func() {
		compareEntities(testutils.TestUpstreamDB, testutils.TestUpstreamDB, &[]models.CheckStatus{})
	})

	ginkgo.It("should have transferred all the canaries", func() {
		compareEntities(testutils.TestUpstreamDB, testutils.TestUpstreamDB, &[]models.Canary{})
	})

	ginkgo.It("should have transferred all the configs", func() {
		compareEntities(testutils.TestUpstreamDB, testutils.TestUpstreamDB, &[]models.ConfigItem{})
	})

	ginkgo.It("should have transferred all the config analyses", func() {
		compareEntities(testutils.TestUpstreamDB, testutils.TestUpstreamDB, &[]models.ConfigAnalysis{})
	})

	ginkgo.It("should have transferred all the config changes", func() {
		compareEntities(testutils.TestUpstreamDB, testutils.TestUpstreamDB, &[]models.ConfigChange{})
	})

	ginkgo.It("should have transferred all the config relationships", func() {
		compareEntities(testutils.TestUpstreamDB, testutils.TestUpstreamDB, &[]models.ComponentRelationship{})
	})

	ginkgo.It("should have transferred all the config component relationships", func() {
		compareEntities(testutils.TestUpstreamDB, testutils.TestUpstreamDB, &[]models.ConfigComponentRelationship{})
	})

	ginkgo.It("should have populated the agents table", func() {
		var count int
		err := testutils.TestUpstreamDB.Select("COUNT(*)").Table("agents").Scan(&count).Error
		Expect(err).ToNot(HaveOccurred())
		Expect(count).ToNot(BeZero())
	})

	ginkgo.It("should have populated the agent_id field", func() {
		var count int
		err := testutils.TestUpstreamDB.Select("COUNT(*)").Table("checks").Where("agent_id IS NOT NULL").Scan(&count).Error
		Expect(err).ToNot(HaveOccurred())
		Expect(count).ToNot(BeZero())
	})
})

// getPrimaryKeys extracts and returns the list of primary keys for the given table from the provided rows.
func getPrimaryKeys(table string, rows any) [][]string {
	var primaryKeys [][]string

	switch table {
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
