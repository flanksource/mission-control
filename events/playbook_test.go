package events

import (
	"encoding/json"

	"github.com/flanksource/duty/fixtures/dummy"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm/clause"
)

var _ = ginkgo.Describe("Should save playbook run on the correct event", ginkgo.Ordered, func() {
	var playbook models.Playbook

	ginkgo.It("should store dummy data", func() {
		dataset := dummy.GetStaticDummyData()
		err := dataset.Populate(playbookDB)
		Expect(err).NotTo(HaveOccurred())
	})

	ginkgo.It("should create a new playbook", func() {
		playbookSpec := v1.PlaybookSpec{
			Description: "write unhealthy component's name to a file",
			On: v1.PlaybookEvent{
				Component: []v1.PlaybookEventDetail{
					{
						Filter: "component.type == 'Entity'",
						Event:  "unhealthy",
						Labels: map[string]string{
							"telemetry": "enabled",
						},
					},
				},
			},
			Actions: []v1.PlaybookAction{
				{
					Name: "write component name to a file",
					Exec: &v1.ExecAction{
						Script: "printf {{.component.name}} > /tmp/component-name.txt",
					},
				},
			},
		}

		spec, err := json.Marshal(playbookSpec)
		Expect(err).NotTo(HaveOccurred())

		playbook = models.Playbook{
			Name:   "unhealthy component writer",
			Spec:   spec,
			Source: models.SourceConfigFile,
		}

		err = playbookDB.Clauses(clause.Returning{}).Create(&playbook).Error
		Expect(err).NotTo(HaveOccurred())
	})

	ginkgo.It("update status to something else other than unhealthy", func() {
		tx := playbookDB.Debug().Model(&models.Component{}).Where("id = ?", dummy.Logistics.ID).UpdateColumn("status", types.ComponentStatusWarning)
		Expect(tx.RowsAffected).To(Equal(int64(1)))

		Expect(tx.Error).NotTo(HaveOccurred())
	})

	ginkgo.It("Expect the event consumer to NOT save a playbook run", func() {
		componentEventConsumer := NewComponentConsumerSync().EventConsumer(playbookDB, playbookDBPool)
		componentEventConsumer.ConsumeEventsUntilEmpty(api.NewContext(playbookDB, nil))

		var playbooks []models.PlaybookRun
		err := playbookDB.Find(&playbooks).Error
		Expect(err).NotTo(HaveOccurred())
		Expect(len(playbooks)).To(Equal(0))
	})

	ginkgo.It("make one of the matching components unhealthy", func() {
		tx := playbookDB.Debug().Model(&models.Component{}).Where("id = ?", dummy.Logistics.ID).UpdateColumn("status", types.ComponentStatusUnhealthy)
		Expect(tx.RowsAffected).To(Equal(int64(1)))

		Expect(tx.Error).NotTo(HaveOccurred())
	})

	ginkgo.It("Expect the event consumer to save the playbook run", func() {
		componentEventConsumer := NewComponentConsumerSync().EventConsumer(playbookDB, playbookDBPool)
		componentEventConsumer.ConsumeEventsUntilEmpty(api.NewContext(playbookDB, nil))

		var playbook models.PlaybookRun
		err := playbookDB.Where("component_id", dummy.Logistics.ID).First(&playbook).Error
		Expect(err).NotTo(HaveOccurred())

		Expect(playbook.Status).To(Equal(models.PlaybookRunStatusScheduled))
	})
})
