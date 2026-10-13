package playbook

import (
	"fmt"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/events"
)

var _ = ginkgo.Describe("Work no person started", func() {
	countRuns := func(playbookID uuid.UUID) int64 {
		ginkgo.GinkgoHelper()
		var count int64
		Expect(DefaultContext.DB().Model(&models.PlaybookRun{}).Where("playbook_id = ?", playbookID).Count(&count).Error).To(Succeed())
		return count
	}

	ginkgo.Describe("a scheduled run", func() {
		scheduled := v1.PlaybookSpec{
			On: &v1.PlaybookTrigger{Schedule: []v1.PlaybookTriggerSchedule{{Schedule: "@every 1h"}}},
			Actions: []v1.PlaybookAction{
				{Name: "echo", Exec: &v1.ExecAction{Script: "echo hi"}},
			},
		}

		ginkgo.It("isn't created without playbook:run on the playbook itself, and the refusal is in its job history", func() {
			playbook := saveScheduledPlaybookForTest("scheduled-unbound", scheduled)

			triggerScheduledRun(DefaultContext, playbook.ID, 0, "@every 1h")

			Expect(countRuns(playbook.ID)).To(BeZero())
			expectScheduledRunJobHistory(playbook.ID, fmt.Sprintf("access denied to subject(%s): cannot run playbook", playbook.ID))
		})

		ginkgo.It("is created as the playbook when it's bound to playbook:run on itself", func() {
			playbook := saveScheduledPlaybookForTest("scheduled-bound", scheduled)
			ginkgo.DeferCleanup(bindPlaybook(playbook, runOnItself(playbook, nil)))

			triggerScheduledRun(DefaultContext, playbook.ID, 0, "@every 1h")

			var run models.PlaybookRun
			Expect(DefaultContext.DB().Where("playbook_id = ?", playbook.ID).First(&run).Error).To(Succeed())
			Expect(run.CreatedBy).To(BeNil(), "no person, and not the System user, started the run")
		})
	})

	ginkgo.Describe("an event-triggered run", ginkgo.Ordered, func() {
		var (
			playbook models.Playbook
			config   models.ConfigItem
		)

		handleWarning := func() {
			ginkgo.GinkgoHelper()
			scheduler := playbookScheduler{Ring: events.NewEventRing(10)}
			Expect(scheduler.Handle(DefaultContext, models.Event{Name: api.EventConfigWarning, EventID: config.ID})).To(Succeed())
		}

		ginkgo.BeforeAll(func() {
			config = models.ConfigItem{
				ID:          uuid.New(),
				Name:        lo.ToPtr("caller-less-target"),
				ConfigClass: models.ConfigClassPod,
				Type:        lo.ToPtr("Kubernetes::Pod"),
			}
			Expect(DefaultContext.DB().Create(&config).Error).To(Succeed())

			playbook = saveScheduledPlaybookForTest("event-triggered", v1.PlaybookSpec{
				On: &v1.PlaybookTrigger{PlaybookTriggerEvents: v1.PlaybookTriggerEvents{
					Config: []v1.PlaybookTriggerEvent{{Event: "warning", Filter: fmt.Sprintf("config.id == '%s'", config.ID)}},
				}},
				Actions: []v1.PlaybookAction{
					{Name: "echo", Exec: &v1.ExecAction{Script: "echo {{.config.name}}"}},
				},
			})
		})

		ginkgo.AfterAll(func() {
			Expect(DefaultContext.DB().Delete(&models.PlaybookRun{}, "playbook_id = ?", playbook.ID).Error).To(Succeed())
			Expect(DefaultContext.DB().Delete(&config).Error).To(Succeed())
		})

		ginkgo.It("isn't created on a target the playbook may not read", func() {
			ginkgo.DeferCleanup(bindPlaybook(playbook, runOnItself(playbook, &allConfigs)))

			handleWarning()

			Expect(countRuns(playbook.ID)).To(BeZero())
			expectScheduledRunJobHistory(playbook.ID, "read access to resource not allowed")
		})

		ginkgo.It("is created on a target the playbook may read", func() {
			ginkgo.DeferCleanup(bindPlaybook(playbook,
				runOnItself(playbook, &allConfigs),
				playbookGrant{Action: policy.ActionRead, Resource: allConfigs},
			))

			handleWarning()

			var run models.PlaybookRun
			Expect(DefaultContext.DB().Where("playbook_id = ?", playbook.ID).First(&run).Error).To(Succeed())
			Expect(run.ConfigID).To(Equal(&config.ID))
			Expect(run.CreatedBy).To(BeNil())
		})
	})
})
