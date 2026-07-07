package playbook

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

var _ = ginkgo.Describe("scheduled playbook runs", func() {
	ginkgo.It("applies defaults and spec timeout through the shared run pipeline", func() {
		playbook := saveScheduledPlaybookForTest("scheduled-defaults", v1.PlaybookSpec{
			Timeout: "2m",
			On: &v1.PlaybookTrigger{
				Schedule: []v1.PlaybookTriggerSchedule{
					{Schedule: "@every 1h"},
				},
			},
			Parameters: []v1.PlaybookParameter{
				{Name: "message", Required: true, Default: "{{.playbook.name}}"},
			},
			Actions: []v1.PlaybookAction{
				{Name: "echo", Exec: &v1.ExecAction{Script: "echo {{.params.message}}"}},
			},
		})

		agent := saveScheduledRunAgentForTest()
		req := RunParams{
			ID:      playbook.ID,
			AgentID: &agent.ID,
		}
		run, err := createPlaybookRun(DefaultContext.WithObject(&playbook, req), &playbook, req, playbookRunOptions{})
		Expect(err).To(Succeed())

		Expect(run.Parameters["message"]).To(Equal(playbook.Name))
		Expect(run.Timeout).To(Equal(2 * time.Minute))
	})

	ginkgo.It("templates explicit schedule parameters before validation", func() {
		playbook := saveScheduledPlaybookForTest("scheduled-templated-params", v1.PlaybookSpec{
			On: &v1.PlaybookTrigger{
				Schedule: []v1.PlaybookTriggerSchedule{
					{
						Schedule: "@every 1h",
						Parameters: map[string]string{
							"message": "{{.playbook.name}}",
						},
					},
				},
			},
			Parameters: []v1.PlaybookParameter{
				{Name: "message", Required: true},
			},
			Actions: []v1.PlaybookAction{
				{Name: "echo", Exec: &v1.ExecAction{Script: "echo {{.params.message}}"}},
			},
		})

		templatedParams, err := templateScheduledRunParams(DefaultContext, &playbook, map[string]string{
			"message": "{{.playbook.name}}",
		})
		Expect(err).To(Succeed())

		agent := saveScheduledRunAgentForTest()
		req := RunParams{
			ID:      playbook.ID,
			AgentID: &agent.ID,
			Params:  templatedParams,
		}
		run, err := createPlaybookRun(DefaultContext.WithObject(&playbook, req), &playbook, req, playbookRunOptions{})
		Expect(err).To(Succeed())
		Expect(run.Parameters["message"]).To(Equal(playbook.Name))
	})

	ginkgo.It("records missing required parameter failures in job history", func() {
		playbook := saveScheduledPlaybookForTest("scheduled-missing-param", v1.PlaybookSpec{
			On: &v1.PlaybookTrigger{
				Schedule: []v1.PlaybookTriggerSchedule{
					{Schedule: "@every 1h"},
				},
			},
			Parameters: []v1.PlaybookParameter{
				{Name: "message", Required: true},
			},
			Actions: []v1.PlaybookAction{
				{Name: "echo", Exec: &v1.ExecAction{Script: "echo {{.params.message}}"}},
			},
		})

		triggerScheduledRun(DefaultContext, playbook.ID, nil)

		var runCount int64
		Expect(DefaultContext.DB().Model(&models.PlaybookRun{}).Where("playbook_id = ?", playbook.ID).Count(&runCount).Error).To(Succeed())
		Expect(runCount).To(BeZero())

		expectScheduledRunJobHistory(playbook.ID, "missing required parameter")
	})

	ginkgo.It("records unknown parameter failures in job history", func() {
		playbook := saveScheduledPlaybookForTest("scheduled-unknown-param", v1.PlaybookSpec{
			On: &v1.PlaybookTrigger{
				Schedule: []v1.PlaybookTriggerSchedule{
					{Schedule: "@every 1h"},
				},
			},
			Actions: []v1.PlaybookAction{
				{Name: "echo", Exec: &v1.ExecAction{Script: "echo hi"}},
			},
		})

		triggerScheduledRun(DefaultContext, playbook.ID, map[string]string{"unexpected": "value"})

		var runCount int64
		Expect(DefaultContext.DB().Model(&models.PlaybookRun{}).Where("playbook_id = ?", playbook.ID).Count(&runCount).Error).To(Succeed())
		Expect(runCount).To(BeZero())

		expectScheduledRunJobHistory(playbook.ID, "unknown parameter")
	})
})

func saveScheduledPlaybookForTest(name string, spec v1.PlaybookSpec) models.Playbook {
	Expect(spec.Validate()).To(Succeed())

	specJSON, err := json.Marshal(spec)
	Expect(err).To(Succeed())

	playbook := models.Playbook{
		ID:        uuid.New(),
		Namespace: "default",
		Name:      fmt.Sprintf("%s-%s", name, uuid.NewString()),
		Spec:      specJSON,
		Source:    models.SourceConfigFile,
	}
	Expect(DefaultContext.DB().Create(&playbook).Error).To(Succeed())

	return playbook
}

func expectScheduledRunJobHistory(playbookID uuid.UUID, expectedError string) {
	var history models.JobHistory
	Expect(DefaultContext.DB().
		Where("name = ? AND resource_type = ? AND resource_id = ?", "SavePlaybookRun", "playbook", playbookID.String()).
		First(&history).Error).To(Succeed())
	Expect(history.Status).To(Equal(models.StatusFailed))
	Expect(history.ErrorCount).To(BeNumerically(">", 0))
	Expect(history.Details["errors"]).To(ContainElement(ContainSubstring(expectedError)))
}

func saveScheduledRunAgentForTest() models.Agent {
	agent := models.Agent{Name: fmt.Sprintf("scheduled-run-test-%s", uuid.NewString())}
	Expect(agent.Save(DefaultContext.DB())).To(Succeed())
	return agent
}
