package playbook

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/flanksource/commons/http"
	dutyApi "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/job"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/flanksource/duty/tests/setup"
	"github.com/flanksource/duty/upstream"
	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/events"
	"github.com/flanksource/incident-commander/playbook/sdk"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"

	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
)

func createPlaybook(name string) (models.Playbook, v1.PlaybookSpec) {
	var spec v1.Playbook
	specContent, err := os.ReadFile(fmt.Sprintf("testdata/%s.yaml", name))
	Expect(err).To(BeNil())

	err = yamlutil.Unmarshal(specContent, &spec)
	Expect(err).To(BeNil())

	specJSON, err := json.Marshal(spec.Spec)
	Expect(err).To(BeNil())

	playbook := &models.Playbook{
		Namespace: "default",
		Name:      spec.Name,
		Spec:      specJSON,
		Source:    models.SourceConfigFile,
	}

	Expect(playbook.Save(DefaultContext.DB())).To(BeNil())
	return *playbook, spec.Spec
}

func ExpectPlaybook(list []api.PlaybookListItem, err error, playbooks ...models.Playbook) {
	Expect(err).To(BeNil())
	Expect(lo.Map(list, func(l api.PlaybookListItem, _ int) string { return l.ID.String() })).
		To(ConsistOf(lo.Map(playbooks, func(p models.Playbook, _ int) string { return p.ID.String() })))
}

func ExpectOKResponse(response *http.Response) {
	var runResp dutyApi.HTTPError
	if err := json.NewDecoder(response.Body).Decode(&runResp); err == nil {
		Expect(runResp.Err).To(BeEmpty())
	}
	Expect(response.IsOK()).To(BeTrue())
}
func ExpectErrorResponse(response *http.Response, err string) {
	var runResp dutyApi.HTTPError
	if err := json.NewDecoder(response.Body).Decode(&runResp); err != nil {
		Expect(runResp.Err).To(Equal(err), runResp)
	}
	Expect(response.IsOK()).To(BeFalse())
}

var _ = Describe("Playbook", func() {

	var _ = Describe("Test Listing | Run API | Approvals", Ordered, func() {
		var (
			configPlaybook    models.Playbook
			checkPlaybook     models.Playbook
			componentPlaybook models.Playbook
			savedRun          models.PlaybookRun
		)

		BeforeAll(func() {
			configPlaybook, _ = createPlaybook("action-approvals")
			checkPlaybook, _ = createPlaybook("action-check")
			componentPlaybook, _ = createPlaybook("action-component")
		})

		Context("api | list playbooks ", func() {
			It("Should fetch the suitable playbook for checks", func() {
				playbooks, err := ListPlaybooksForCheck(DefaultContext, dummy.LogisticsAPIHealthHTTPCheck.ID.String())
				ExpectPlaybook(playbooks, err, checkPlaybook)

				playbooks, err = ListPlaybooksForCheck(DefaultContext, dummy.LogisticsDBCheck.ID.String())
				ExpectPlaybook(playbooks, err)
			})

			It("Should fetch the suitable playbook for components", func() {
				playbooks, err := ListPlaybooksForComponent(DefaultContext, dummy.Logistics.ID.String())
				ExpectPlaybook(playbooks, err, componentPlaybook)

				playbooks, err = ListPlaybooksForComponent(DefaultContext, dummy.LogisticsUI.ID.String())
				ExpectPlaybook(playbooks, err)
			})

			It("Should fetch the suitable playbook for configs", func() {
				playbooks, err := ListPlaybooksForConfig(DefaultContext, dummy.EKSCluster.ID.String())
				ExpectPlaybook(playbooks, err, configPlaybook)

				playbooks, err = ListPlaybooksForConfig(DefaultContext, dummy.KubernetesCluster.ID.String())
				ExpectPlaybook(playbooks, err)
			})
		})

		Context("parameter validation", func() {
			testData := []struct {
				name          string
				expectedError string
				param         map[string]string
			}{
				{
					name:          "must validate required parameters",
					expectedError: "missing required parameter(s): path",
					param: map[string]string{
						"name": "test",
					},
				},
				{
					name:          "must validate unknown parameters",
					expectedError: "unknown parameter(s): icon",
					param: map[string]string{
						"path": "test",
						"name": "test",
						"icon": "flux",
					},
				},
			}

			for _, td := range testData {
				It(td.name, func() {
					response, err := client.Run(sdk.RunParams{
						ID:       configPlaybook.ID.String(),
						ConfigID: dummy.EKSCluster.ID.String(),
						Params:   td.param,
					})

					if td.expectedError != "" {
						Expect(err).ToNot(BeNil())
						Expect(err.Error()).To(Equal(td.expectedError))
					} else {
						Expect(err).To(BeNil())
						Expect(response.RunID).ToNot(BeEmpty())
					}
				})
			}
		})

		It("should store playbook run via API", func() {
			run := sdk.RunParams{
				ID:       configPlaybook.ID,
				ConfigID: dummy.EKSCluster.ID,
				Params: map[string]string{
					"path": tempPath,
					// "footer": "" // exclude this so we use the default value
				},
			}

			response, err := client.Run(run)
			Expect(err).To(BeNil())
			Expect(response).NotTo(BeNil())
			Expect(response.RunID).NotTo(BeEmpty())

			Expect(DefaultContext.DB().Where("id = ? ", response.RunID).First(&savedRun).Error).To(BeNil())

			Expect(savedRun.PlaybookID).To(Equal(configPlaybook.ID), "run should have been created for the correct playbook")
			Expect(*savedRun.CreatedBy).To(Equal(dummy.JohnDoe.ID), "run should have been created by the authenticated person")
		})

		It("should have auto approved & scheduled the playbook run", func() {
			events.ConsumeAll(DefaultContext)

			waitFor(&savedRun, models.PlaybookRunStatusCompleted)
		})

		It("should ensure that the action worked correctly", func() {

			actions, err := savedRun.GetActions(DefaultContext.DB())
			Expect(err).To(BeNil())
			Expect(actions).To(HaveLen(2))

			f, err := os.ReadFile(tempPath)
			Expect(err).To(BeNil())

			Expect(string(f)).To(Equal(fmt.Sprintf("id=%s\n%s", dummy.EKSCluster.ID, *dummy.EKSCluster.Name)))
		})
	})

	var _ = Describe("parameters", Ordered, func() {
		var (
			testPlaybook models.Playbook
			savedRun     models.PlaybookRun
			tempDir      string
			tempFile     string
		)

		BeforeAll(func() {
			var err error
			tempDir, err = os.MkdirTemp("", "playbook-*")
			Expect(err).To(BeNil())
			tempFile = filepath.Join(tempDir, "test.txt")

			testPlaybook, _ = createPlaybook("action-params")
		})

		AfterAll(func() {
			_ = os.RemoveAll(tempDir)
		})

		It("should store playbook run via API", func() {
			run := sdk.RunParams{
				ID: testPlaybook.ID,
				Params: map[string]string{
					"path":         tempFile,
					"my_config":    dummy.EKSCluster.ID.String(),
					"my_component": dummy.Logistics.ID.String(),
				},
			}

			response, err := client.Run(run)
			Expect(err).To(BeNil())

			err = DefaultContext.DB().Where("id = ? ", response.RunID).First(&savedRun).Error
			Expect(err).To(BeNil())

			Expect(savedRun.PlaybookID).To(Equal(testPlaybook.ID), "run should have been created for the correct playbook")
			Expect(*savedRun.CreatedBy).To(Equal(dummy.JohnDoe.ID), "run should have been created by the authenticated person")
		})

		It("should have correctly used config & component fields from parameters", func() {

			waitFor(&savedRun)

			f, err := os.ReadFile(tempFile)
			Expect(err).To(BeNil())
			Expect(string(f)).To(Equal(fmt.Sprintf("%s\n%s\n", dummy.EKSCluster.ConfigClass, dummy.Logistics.Name)))
		})
	})

	var _ = Describe("action filters", Ordered, func() {
		var (
			spec     v1.PlaybookSpec
			playbook models.Playbook
			run      *models.PlaybookRun
			dataFile = "/tmp/action-filter-test.txt"
			logFile  = "/tmp/action-filter-test-log.txt"
		)

		BeforeAll(func() {
			playbook, spec = createPlaybook("action-filter")
		})

		It("should execute the playbook", func() {
			run = runPlaybook(playbook, RunParams{
				ConfigID: dummy.EKSCluster.ID,
				Params: map[string]string{
					"path":     dataFile,
					"log_path": logFile,
				},
			})
		})

		It("should have correctly ran some and skipped some of the actions", func() {
			var actions []models.PlaybookRunAction
			err := DefaultContext.DB().Where("playbook_run_id = ?", run.ID).Find(&actions).Error
			Expect(err).To(BeNil())

			Expect(len(actions)).To(Equal(len(spec.Actions)), "All the actions must have run event if some failed")

			for _, action := range actions {
				switch action.Name {
				case "Create the file":
					Expect(action.Status).To(Equal(models.PlaybookActionStatusCompleted), fmt.Sprintf("Create the file: %s", models.PlaybookActionStatusCompleted))

				case "Log if the file creation failed":
					Expect(action.Status).To(Equal(models.PlaybookActionStatusSkipped), fmt.Sprintf("Log if the file creation failed: %s", models.PlaybookActionStatusSkipped))

				case "Log if the file creation succeeded":
					Expect(action.Status).To(Equal(models.PlaybookActionStatusCompleted), fmt.Sprintf("Log if the file creation succeeded: %s", models.PlaybookActionStatusCompleted))

				case "Run a non existing command":
					Expect(action.Status).To(Equal(models.PlaybookActionStatusFailed), fmt.Sprintf("Run a non existing command: %s", models.PlaybookActionStatusFailed))

				case "Log if the command failed":
					Expect(action.Status).To(Equal(models.PlaybookActionStatusCompleted), fmt.Sprintf("Log if the command failed: %s", models.PlaybookActionStatusCompleted))

				case "Skip if cluster config":
					Expect(action.Status).To(Equal(models.PlaybookActionStatusSkipped), fmt.Sprintf("Skip if cluster config: %s", models.PlaybookActionStatusSkipped))

				case "Skip this command":
					Expect(action.Status).To(Equal(models.PlaybookActionStatusSkipped), fmt.Sprintf("Skip this command: %s", models.PlaybookActionStatusSkipped))

				case "Log the end of the playbook":
					Expect(action.Status).To(Equal(models.PlaybookActionStatusCompleted), fmt.Sprintf("Log the end of the playbook: %s", models.PlaybookActionStatusCompleted))
				}
			}
		})

		It("should have populated the files correctly", func() {
			data, err := os.ReadFile(dataFile)
			Expect(err).To(BeNil())
			Expect(string(data)).To(Equal(models.ConfigClassCluster))

			logs, err := os.ReadFile(logFile)
			Expect(err).To(BeNil())
			Expect(string(logs)).To(Equal("File creation succeeded\nCommand failed\n==end==\n"))
		})
	})

	var _ = Describe("function | last result", Ordered, func() {
		var (
			spec     v1.PlaybookSpec
			playbook models.Playbook
			run      *models.PlaybookRun
			dataFile = "/tmp/access-previous-result.txt"
		)

		BeforeAll(func() {
			playbook, spec = createPlaybook("action-last-result")
		})

		It("should store playbook run via API", func() {
			run = runPlaybook(playbook, RunParams{
				ConfigID: dummy.EKSCluster.ID,
				Params: map[string]string{
					"path": dataFile,
				},
			})
		})

		It("should have correctly ran some and skipped some of the actions", func() {
			var actions []models.PlaybookRunAction
			err := DefaultContext.DB().Where("playbook_run_id = ?", run.ID).Find(&actions).Error
			Expect(err).To(BeNil())

			Expect(len(actions)).To(Equal(len(spec.Actions)), "All the actions must have run event if some failed")

			expectedStatus := map[string]models.PlaybookActionStatus{
				"make dummy API call":        models.PlaybookActionStatusCompleted,
				"Notify if the count is low": models.PlaybookActionStatusSkipped,
				"Log if count is high":       models.PlaybookActionStatusCompleted,
				"Save the count":             models.PlaybookActionStatusCompleted,
			}

			for _, action := range actions {
				expected, exists := expectedStatus[action.Name]
				if !exists {
					Fail("Unexpected action: " + action.Name)
					continue
				}

				Expect(action.Status).To(Equal(expected), action.Name)
			}
		})

		It("should have populated the files correctly", func() {
			data, err := os.ReadFile(dataFile)
			Expect(err).To(BeNil())
			Expect(string(data)).To(Equal("HIGH\n20"))
		})
	})

	var _ = Describe("runners", Ordered, Label("slow"), func() {
		var (
			spec     v1.PlaybookSpec
			playbook models.Playbook
			run      *models.PlaybookRun

			upstreamConfig upstream.UpstreamConfig
			agentName      = "aws-agent"
			awsAgent       models.Agent

			agentContext context.Context
			agentDBDrop  func()
		)

		BeforeAll(func() {
			playbook, spec = createPlaybook("agent-runner")

			// Setup agent
			newCtx, drop, err := setup.NewDB(DefaultContext, "aws")
			Expect(err).To(BeNil())
			agentContext = *newCtx
			agentDBDrop = drop
			agentContext = agentContext.WithName("agent").WithDBLogger("agent", "info")

			upstreamConfig = upstream.UpstreamConfig{
				AgentName: "aws",
				Host:      server.URL,
				Username:  agentName,
				Password:  "dummy",
			}

			// save the agent to the db
			agentPerson := &models.Person{Name: agentName}
			Expect(agentPerson.Save(DefaultContext.DB())).To(BeNil())

			awsAgent = models.Agent{Name: "aws", PersonID: &agentPerson.ID}
			Expect((&awsAgent).Save(DefaultContext.DB())).To(BeNil())

		})

		AfterAll(func() {
			if agentDBDrop != nil {
				agentDBDrop()
			}
		})

		It("should execute the playbook", func() {
			run = runPlaybook(playbook, RunParams{
				ConfigID: dummy.KubernetesNodeA.ID,
			}, models.PlaybookRunStatusScheduled, models.PlaybookRunStatusWaiting)

			action, err := run.GetAction(DefaultContext.DB(), spec.Actions[0].Name)
			Expect(err).To(BeNil())
			// first step schedules on local
			Expect(action.AgentID).To(BeNil())

			waitFor(run, models.PlaybookRunStatusWaiting)
			action, err = run.GetAction(DefaultContext.DB(), spec.Actions[1].Name)
			Expect(err).To(BeNil())
			// second step runs on agent
			Expect(*action.AgentID).To(Equal(awsAgent.ID))
			Expect(action.Status).To(Equal(models.PlaybookActionStatusWaiting))

		})

		It("should pull the action from the upstream", func() {
			Expect(PullPlaybookAction(job.New(agentContext), upstreamConfig)).To(BeNil())

			action, err := run.GetAgentAction(agentContext.DB(), spec.Actions[1].Name)
			Expect(err).To(BeNil())

			Expect(action.Status).To(Equal(models.PlaybookActionStatusWaiting))

		})

		It("should run the pulled action on the agent", func() {
			err := StartPlaybookConsumers(agentContext)
			Expect(err).To(BeNil())

			_, err = ActionAgentConsumer(agentContext)
			Expect(err).To(BeNil())

			Eventually(func() models.PlaybookActionStatus {
				action, _ := run.GetAgentAction(agentContext.DB(), spec.Actions[1].Name)
				if action != nil {
					return action.Status
				}
				return "unknown"
			}, "10s", "1s").Should(Equal(models.PlaybookActionStatusCompleted))
		})

		It("should push the action result to the upstream", func() {
			pushed, err := PushPlaybookActions(agentContext, upstreamConfig, 10)
			Expect(err).To(BeNil())
			Expect(pushed).To(Equal(1))
		})

		It("should ensure that the playbook ran to completion", func() {
			waitFor(run, models.PlaybookRunStatusCompleted)
		})

		It("should ensure that the playbook ran correctly", func() {
			actions, err := run.GetActions(DefaultContext.DB())
			Expect(err).To(BeNil())

			Expect(len(actions)).To(Equal(2))
			for i := range actions {
				Expect(actions[i].Status).To(Equal(models.PlaybookActionStatusCompleted))
				Expect(actions[i].Result["stdout"]).To(Equal(dummy.KubernetesNodeA.ConfigClass))
			}
		})
	})

	var _ = Describe("actions", func() {
		It("exec | powershell", func() {
			run := createAndRun("exec-powershell", RunParams{
				ConfigID: dummy.KubernetesNodeA.ID,
			})
			Expect(run.Status).To(Equal(models.PlaybookRunStatusCompleted), run.String(DefaultContext.DB()))
			actions, err := run.GetActions(DefaultContext.DB())
			Expect(err).To(BeNil())
			Expect(len(actions)).To(Equal(2))
			Expect(actions[0].JSON()["item"]).To(Equal(*dummy.KubernetesNodeA.Name))
			Expect(actions[1].JSON()["item"]).To(Equal(fmt.Sprintf("name=%s", *dummy.KubernetesNodeA.Name)))

		})
	})
	type testData struct {
		name        string
		description string
		status      models.PlaybookRunStatus
		params      RunParams
		extra       func(run *models.PlaybookRun)
	}

	tests := []testData{
		{
			name:        "bad-action-spec",
			status:      models.PlaybookRunStatusFailed,
			description: "invalid action spec should fail",
			extra: func(run *models.PlaybookRun) {
				var action models.PlaybookRunAction
				err := DefaultContext.DB().Where("playbook_run_id = ? ", run.ID).First(&action).Error
				Expect(err).To(BeNil())
				Expect(lo.FromPtrOr(action.Error, "")).NotTo(BeEmpty())
				Expect(action.Status).To(Equal(models.PlaybookActionStatusFailed))
			},
		},
		{
			name:        "bad-spec",
			status:      models.PlaybookRunStatusFailed,
			description: "invalid spec should fail",
			extra: func(run *models.PlaybookRun) {
				Expect(run.Error).ToNot(BeNil())
			},
		},
	}
	for _, test := range tests {
		It(test.description, func() {
			run := createAndRun(test.name, test.params, test.status)
			if test.extra != nil {
				test.extra(run)
			}
		})
	}
})

func createAndRun(name string, params RunParams, statuses ...models.PlaybookRunStatus) *models.PlaybookRun {
	playbook, _ := createPlaybook(name)
	return runPlaybook(playbook, params, statuses...)
}

func runPlaybook(playbook models.Playbook, params RunParams, statuses ...models.PlaybookRunStatus) *models.PlaybookRun {
	run, err := Run(DefaultContext, &playbook, params)
	Expect(err).To(BeNil())
	return waitFor(run, statuses...)
}

func waitFor(run *models.PlaybookRun, statuses ...models.PlaybookRunStatus) *models.PlaybookRun {
	s := append([]models.PlaybookRunStatus{}, statuses...)
	if len(s) == 0 {
		s = append(s, models.PlaybookRunStatusFailed, models.PlaybookRunStatusCompleted)
	}

	var savedRun *models.PlaybookRun
	Eventually(func(g Gomega) models.PlaybookRunStatus {
		err := DefaultContext.DB().Where("id = ? ", run.ID).First(&savedRun).Error
		Expect(err).ToNot(HaveOccurred())

		events.ConsumeAll(DefaultContext)
		_, err = RunConsumer(DefaultContext)
		if err != nil {
			DefaultContext.Errorf("%+v", err)
		}

		if savedRun != nil {
			return savedRun.Status
		}
		return models.PlaybookRunStatus("Unknown")

	}).WithTimeout(15 * time.Second).WithPolling(time.Second).Should(BeElementOf(s))

	return savedRun
}
