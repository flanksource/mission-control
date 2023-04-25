package upstream

import (
	"fmt"

	"github.com/flanksource/duty/fixtures/dummy"
	"github.com/flanksource/incident-commander/api"
	pkgEvents "github.com/flanksource/incident-commander/events"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

// 1. Initial setup
// 	a. Fire up two databases with migrations ran on both of them
// 	b. Fire up an HTTP Server for the upstream
// 2. Update the related tables on database A
// 3. Check that those changes are reflected on the event_queue table
// 4. Setup event handler & provide upstream's configuration
// 5. Now, verify those changes on the upstream's database

var _ = ginkgo.Describe("Track changes on the event_queue tabe", ginkgo.Ordered, func() {
	ginkgo.It("should track changes on the event_queue table", func() {
		var events []api.Event
		err := testDB.Where("name = ?", pkgEvents.EventPushQueueCreate).Find(&events).Error
		Expect(err).NotTo(HaveOccurred())

		groupedEvents := pkgEvents.GroupChangelogsByTables(events)
		for table, itemIDs := range groupedEvents {
			switch table {
			case "canaries":
				Expect(len(itemIDs)).To(Equal(len(dummy.AllDummyCanaries)))

			case "checks":
				Expect(len(itemIDs)).To(Equal(len(dummy.AllDummyChecks)))

			case "components":
				Expect(len(itemIDs)).To(Equal(len(dummy.AllDummyComponents)))

			case "config_items":
				Expect(len(itemIDs)).To(Equal(len(dummy.AllDummyConfigs)))

			case "config_analysis":
				Expect(len(itemIDs)).To(Equal(len(dummy.AllDummyConfigAnalysis)))

			case "check_statuses":
				Expect(len(itemIDs)).To(Equal(len(dummy.AllDummyCheckStatuses)))

			case "component_relationships":
				Expect(len(itemIDs)).To(Equal(len(dummy.AllDummyComponentRelationships)))

			case "config_component_relationships":
				Expect(len(itemIDs)).To(Equal(len(dummy.AllDummyConfigComponentRelationships)))

			default:
				ginkgo.Fail(fmt.Sprintf("Unexpected table %s on the event queue for %s", table, pkgEvents.EventPushQueueCreate))
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
