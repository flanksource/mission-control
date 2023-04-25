package upstream

import (
	"fmt"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/fixtures/dummy"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	pkgEvents "github.com/flanksource/incident-commander/events"
)

// 1. Initial setup
// 	a. Fire up two databases with migrations ran on both of them
// 	b. Fire up an HTTP Server for the upstream
// 2. Update the related tables on database A
// 3. Check that those changes are reflected on the event_queue table
// 4. Setup event handler & provide upstream's configuration
// 5. Now, verify those changes on the upstream's database

var _ = ginkgo.Describe("Track changes on the event_queue table", ginkgo.Ordered, func() {
	ginkgo.It("should track insertion on the event_queue table", func() {
		var events []api.Event
		err := testDB.Where("name = ?", pkgEvents.EventPushQueueCreate).Find(&events).Error
		Expect(err).NotTo(HaveOccurred())

		groupedEvents := pkgEvents.GroupChangelogsByTables(events)
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

			default:
				ginkgo.Fail(fmt.Sprintf("Unexpected table %q on the event queue for %q", table, pkgEvents.EventPushQueueCreate))
			}
		}

		Expect(len(events)).To(Equal(
			len(dummy.AllDummyCanaries) +
				len(dummy.AllDummyChecks) +
				len(dummy.AllDummyComponents) +
				len(dummy.AllDummyConfigs) +
				len(dummy.AllDummyConfigAnalysis) +
				len(dummy.AllDummyCheckStatuses) +
				len(dummy.AllDummyComponentRelationships) +
				len(dummy.AllDummyConfigComponentRelationships)))
	})

	ginkgo.It("should track updates & deletes on the event_queue table", func() {
		start := time.Now()

		modifiedNewDummy := dummy.Logistics
		modifiedNewDummy.ID = uuid.New()

		err := testDB.Create(&modifiedNewDummy).Error
		Expect(err).NotTo(HaveOccurred())

		modifiedNewDummy.Status = models.ComponentStatusUnhealthy
		err = testDB.Save(&modifiedNewDummy).Error
		Expect(err).NotTo(HaveOccurred())

		modifiedNewDummy.Status = models.ComponentStatusUnhealthy
		err = testDB.Delete(&modifiedNewDummy).Error
		Expect(err).NotTo(HaveOccurred())

		var events []api.Event
		err = testDB.Where("name = ? AND created_at >= ?", pkgEvents.EventPushQueueCreate, start).Find(&events).Error
		Expect(err).NotTo(HaveOccurred())

		Expect(len(events)).To(Equal(3))

		groupedEvents := pkgEvents.GroupChangelogsByTables(events)
		Expect(groupedEvents["components"]).To(Equal([][]string{{modifiedNewDummy.ID.String()}, {modifiedNewDummy.ID.String()}, {modifiedNewDummy.ID.String()}}))
	})

	ginkgo.It("Setup http server for upstream", func() {
		testEchoServer = echo.New()
		testEchoServer.POST("/upstream_push", PushUpstream)
		listenAddr := fmt.Sprintf(":%d", testUpstreamServerPort)
		logger.Infof("Listening on %s", listenAddr)
		db.Gorm = testUpstreamDB
		go func() {
			err := testEchoServer.Start(listenAddr)
			Expect(err).NotTo(HaveOccurred())
		}()
	})

	// ginkgo.It("start streaming events", func() {
	// 	eventHandlerConfig := events.Config{
	// 		UpstreamConf: api.UpstreamConfig{
	// 			ClusterName: "test-cluster",
	// 			URL:         fmt.Sprintf("http://localhost:%d/upstream_push", testUpstreamServerPort),
	// 			Username:    "admin",
	// 			Password:    "admin",
	// 			Labels:      []string{"test"},
	// 		},
	// 	}
	// 	events.ListenForEvents(context.Background(), testDB, eventHandlerConfig)
	// })
})

func populateMonitoredTables(gormDB *gorm.DB) error {
	for _, c := range dummy.AllDummyCanaries {
		err := gormDB.Create(&c).Error
		if err != nil {
			return err
		}
	}

	for _, c := range dummy.AllDummyChecks {
		err := gormDB.Create(&c).Error
		if err != nil {
			return err
		}
	}

	for _, c := range dummy.AllDummyComponents {
		err := gormDB.Create(&c).Error
		if err != nil {
			return err
		}
	}

	for _, c := range dummy.AllDummyConfigs {
		err := gormDB.Create(&c).Error
		if err != nil {
			return err
		}
	}

	for _, c := range dummy.AllDummyConfigAnalysis {
		err := gormDB.Create(&c).Error
		if err != nil {
			return err
		}
	}

	for _, c := range dummy.AllDummyCheckStatuses {
		err := gormDB.Create(&c).Error
		if err != nil {
			return err
		}
	}

	for _, c := range dummy.AllDummyConfigComponentRelationships {
		err := gormDB.Create(&c).Error
		if err != nil {
			return err
		}
	}

	for _, c := range dummy.AllDummyComponentRelationships {
		err := gormDB.Create(&c).Error
		if err != nil {
			return err
		}
	}

	// TODO:
	// - config changes
	// - config relationships

	return nil
}

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
