// Tests for the action consumer's claim-then-execute split and the orphaned-action reaper.
package playbook

import (
	"time"

	"github.com/flanksource/duty/models"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("Orphaned action reaper", func() {
	var run models.PlaybookRun

	newAction := func(status models.PlaybookActionStatus, startTime time.Time) models.PlaybookRunAction {
		action := models.PlaybookRunAction{
			PlaybookRunID: run.ID,
			Name:          "step",
			Status:        status,
			StartTime:     startTime,
			// scheduled in the future so the background consumer never picks it up,
			// keeping assertions free of races.
			ScheduledTime: time.Now().Add(time.Hour),
		}
		Expect(DefaultContext.DB().Create(&action).Error).To(BeNil())
		return action
	}

	ginkgo.BeforeEach(func() {
		playbook := models.Playbook{
			Namespace: "default",
			Name:      "reaper-test",
			Spec:      []byte(`{"actions":[]}`),
			Source:    models.SourceConfigFile,
		}
		Expect(playbook.Save(DefaultContext.DB())).To(BeNil())

		run = models.PlaybookRun{
			PlaybookID:    playbook.ID,
			Spec:          playbook.Spec,
			Status:        models.PlaybookRunStatusRunning,
			ScheduledTime: time.Now(),
			Timeout:       30 * time.Minute,
		}
		Expect(DefaultContext.DB().Create(&run).Error).To(BeNil())
	})

	ginkgo.It("resets a running action whose start_time is past the orphan timeout", func() {
		action := newAction(models.PlaybookActionStatusRunning, time.Now().Add(-2*time.Hour))

		Expect(ReapOrphanedActions(DefaultContext)).To(BeNil())

		var got models.PlaybookRunAction
		Expect(DefaultContext.DB().Where("id = ?", action.ID).First(&got).Error).To(BeNil())
		Expect(got.Status).To(Equal(models.PlaybookActionStatusScheduled))
		Expect(got.StartTime.IsZero()).To(BeTrue())
	})

	ginkgo.It("resets an orphaned agent-pulled action to waiting", func() {
		action := newAction(models.PlaybookActionStatusRunning, time.Now().Add(-2*time.Hour))
		// Agent-pulled actions have a null playbook_run_id (PullPlaybookAction omits it),
		// so they must be reset to waiting for ActionAgentConsumer to re-pick them.
		Expect(DefaultContext.DB().Exec(
			"UPDATE playbook_run_actions SET playbook_run_id = NULL WHERE id = ?", action.ID).Error).To(BeNil())

		Expect(ReapOrphanedActions(DefaultContext)).To(BeNil())

		var got models.PlaybookRunAction
		Expect(DefaultContext.DB().Where("id = ?", action.ID).First(&got).Error).To(BeNil())
		Expect(got.Status).To(Equal(models.PlaybookActionStatusWaiting))
		Expect(got.StartTime.IsZero()).To(BeTrue())
	})

	ginkgo.It("leaves a recently started running action untouched", func() {
		action := newAction(models.PlaybookActionStatusRunning, time.Now())

		Expect(ReapOrphanedActions(DefaultContext)).To(BeNil())

		var got models.PlaybookRunAction
		Expect(DefaultContext.DB().Where("id = ?", action.ID).First(&got).Error).To(BeNil())
		Expect(got.Status).To(Equal(models.PlaybookActionStatusRunning))
	})

	ginkgo.It("does not touch actions in a terminal state", func() {
		action := newAction(models.PlaybookActionStatusCompleted, time.Now().Add(-2*time.Hour))

		Expect(ReapOrphanedActions(DefaultContext)).To(BeNil())

		var got models.PlaybookRunAction
		Expect(DefaultContext.DB().Where("id = ?", action.ID).First(&got).Error).To(BeNil())
		Expect(got.Status).To(Equal(models.PlaybookActionStatusCompleted))
	})
})
