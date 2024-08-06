package playbook

import (
	"encoding/json"
	"fmt"
	netHTTP "net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/flanksource/commons/http"
	dutyApi "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/flanksource/duty/tests/setup"
	"github.com/flanksource/duty/upstream"
	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/events"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	"gopkg.in/yaml.v3"
	"gorm.io/gorm/clause"
)

func createPlaybook(name string) (models.Playbook, v1.PlaybookSpec) {
	var spec v1.PlaybookSpec
	specContent, err := os.ReadFile(fmt.Sprintf("testdata/%s.yaml", name))
	Expect(err).NotTo(HaveOccurred())

	err = yaml.Unmarshal(specContent, &spec)
	Expect(err).NotTo(HaveOccurred())

	specJSON, err := json.Marshal(spec)
	Expect(err).NotTo(HaveOccurred())

	playbook := models.Playbook{
		Name:   name,
		Spec:   specJSON,
		Source: models.SourceConfigFile,
	}

	err = DefaultContext.DB().Clauses(clause.Returning{}).Create(&playbook).Error
	Expect(err).NotTo(HaveOccurred())
	return playbook, spec
}

func ExpectPlaybook(list []api.PlaybookListItem, err error, playbooks ...models.Playbook) {
	Expect(err).NotTo(HaveOccurred())
	Expect(lo.Map(list, func(l api.PlaybookListItem, _ int) string { return l.ID.String() })).
		To(ConsistOf(lo.Map(playbooks, func(p models.Playbook, _ int) string { return p.ID.String() })))
}

var _ = ginkgo.Describe("Playbook", func() {
	var _ = ginkgo.Describe("Test Listing | Run API | Approvals", ginkgo.Ordered, func() {
		var (
			configPlaybook    models.Playbook
			checkPlaybook     models.Playbook
			componentPlaybook models.Playbook
			runResp           RunResponse
		)

		ginkgo.BeforeAll(func() {
			configPlaybook, _ = createPlaybook("action-approvals")
			checkPlaybook, _ = createPlaybook("action-check")
			componentPlaybook, _ = createPlaybook("action-component")

		})

		ginkgo.It("Should fetch the suitable playbook for checks", func() {
			playbooks, err := ListPlaybooksForCheck(DefaultContext, dummy.LogisticsAPIHealthHTTPCheck.ID.String())
			ExpectPlaybook(playbooks, err, checkPlaybook)

			playbooks, err = ListPlaybooksForCheck(DefaultContext, dummy.LogisticsDBCheck.ID.String())
			ExpectPlaybook(playbooks, err)
		})

		ginkgo.It("Should fetch the suitable playbook for components", func() {
			playbooks, err := ListPlaybooksForComponent(DefaultContext, dummy.Logistics.ID.String())
			ExpectPlaybook(playbooks, err, componentPlaybook)

			playbooks, err = ListPlaybooksForComponent(DefaultContext, dummy.LogisticsUI.ID.String())
			ExpectPlaybook(playbooks, err)
		})

		ginkgo.It("Should fetch the suitable playbook for configs", func() {
			playbooks, err := ListPlaybooksForConfig(DefaultContext, dummy.EKSCluster.ID.String())
			ExpectPlaybook(playbooks, err, configPlaybook)

			playbooks, err = ListPlaybooksForConfig(DefaultContext, dummy.KubernetesCluster.ID.String())
			ExpectPlaybook(playbooks, err)
		})

		ginkgo.Context("parameter validation", func() {
			testData := []struct {
				name          string
				expectedError string
				param         map[string]string
			}{
				{
					name:          "must validate required parameters",
					expectedError: "missing required parameter(s): path",
					param: map[string]string{
						"footer": "test",
					},
				},
				{
					name:          "must validate unknown parameters",
					expectedError: "unknown parameter(s): icon",
					param: map[string]string{
						"path": "test",
						"icon": "flux",
					},
				},
			}

			for _, td := range testData {
				ginkgo.It(td.name, func() {
					run := RunParams{
						ID:       configPlaybook.ID,
						ConfigID: dummy.EKSCluster.ID,
						Params:   td.param,
					}

					httpClient := http.NewClient().Auth(dummy.JohnDoe.Name, "admin").BaseURL(fmt.Sprintf("http://localhost:%d/playbook", echoServerPort))
					resp, err := httpClient.R(DefaultContext).Header("Content-Type", "application/json").Post("/run", run)
					Expect(err).NotTo(HaveOccurred())

					Expect(resp.StatusCode).To(Equal(netHTTP.StatusBadRequest))

					var runResp dutyApi.HTTPError
					err = json.NewDecoder(resp.Body).Decode(&runResp)
					Expect(err).NotTo(HaveOccurred())

					Expect(runResp.Err).To(Equal(td.expectedError))
				})
			}
		})

		ginkgo.It("should store playbook run via API", func() {
			run := RunParams{
				ID:       configPlaybook.ID,
				ConfigID: dummy.EKSCluster.ID,
				Params: map[string]string{
					"path": tempPath,
					// "footer": "" // exclude this so we use the default value
				},
			}

			httpClient := http.NewClient().Auth(dummy.JohnDoe.Name, "admin").BaseURL(fmt.Sprintf("http://localhost:%d/playbook", echoServerPort))
			resp, err := httpClient.R(DefaultContext).Header("Content-Type", "application/json").Post("/run", run)
			Expect(err).NotTo(HaveOccurred())

			Expect(resp.StatusCode).To(Equal(netHTTP.StatusCreated))

			err = json.NewDecoder(resp.Body).Decode(&runResp)
			Expect(err).NotTo(HaveOccurred())

			var savedRun models.PlaybookRun
			err = DefaultContext.DB().Where("id = ? ", runResp.RunID).First(&savedRun).Error
			Expect(err).NotTo(HaveOccurred())

			Expect(savedRun.PlaybookID).To(Equal(configPlaybook.ID), "run should have been created for the correct playbook")
			Expect(*savedRun.CreatedBy).To(Equal(dummy.JohnDoe.ID), "run should have been created by the authenticated person")
		})

		ginkgo.It("should have auto approved & scheduled the playbook run", func() {
			events.ConsumeAll(DefaultContext)

			Eventually(func() models.PlaybookRunStatus {
				var savedRun *models.PlaybookRun
				if err := DefaultContext.DB().Select("status").Where("id = ? ", runResp.RunID).First(&savedRun).Error; err != nil {
					Expect(err).To(BeNil())
				}
				if savedRun != nil {
					return savedRun.Status
				}
				return models.PlaybookRunStatusPending
			}, "15s").Should(Equal(models.PlaybookRunStatusCompleted))
		})

		ginkgo.It("should execute playbook", func() {
			waitFor(&models.PlaybookRun{
				ID: uuid.MustParse(runResp.RunID),
			})
		})

		ginkgo.It("should ensure that the action worked correctly", func() {
			var runActions []models.PlaybookRunAction
			err := DefaultContext.DB().Where("playbook_run_id = ?", runResp.RunID).Find(&runActions).Error
			Expect(err).NotTo(HaveOccurred())

			d, _ := json.Marshal(runActions)
			Expect(len(runActions)).To(Equal(2))
			fmt.Println(string(d))

			f, err := os.ReadFile(tempPath)
			Expect(err).NotTo(HaveOccurred())

			Expect(string(f)).To(Equal(fmt.Sprintf("id=%s\n%s", dummy.EKSCluster.ID, dummy.EKSCluster.ConfigClass)))
		})
	})

	var _ = ginkgo.Describe("Test playbook parameters", ginkgo.Ordered, func() {
		var (
			testPlaybook models.Playbook
			runResp      RunResponse
			tempDir      string
			tempFile     string
		)

		ginkgo.BeforeAll(func() {
			var err error
			tempDir, err = os.MkdirTemp("", "playbook-*")
			Expect(err).NotTo(HaveOccurred())
			tempFile = filepath.Join(tempDir, "test.txt")

			testPlaybook, _ = createPlaybook("action-params")
		})

		ginkgo.AfterAll(func() {
			_ = os.RemoveAll(tempDir)
		})

		ginkgo.It("should store playbook run via API", func() {
			run := RunParams{
				ID: testPlaybook.ID,
				Params: map[string]string{
					"path":         tempFile,
					"my_config":    dummy.EKSCluster.ID.String(),
					"my_component": dummy.Logistics.ID.String(),
				},
			}

			httpClient := http.NewClient().Auth(dummy.JohnDoe.Name, "admin").BaseURL(fmt.Sprintf("http://localhost:%d/playbook", echoServerPort))
			resp, err := httpClient.R(DefaultContext).Header("Content-Type", "application/json").Post("/run", run)
			Expect(err).NotTo(HaveOccurred())

			Expect(resp.StatusCode).To(Equal(netHTTP.StatusCreated))

			Expect(json.NewDecoder(resp.Body).Decode(&runResp)).NotTo(HaveOccurred())

			var savedRun models.PlaybookRun
			err = DefaultContext.DB().Where("id = ? ", runResp.RunID).First(&savedRun).Error
			Expect(err).NotTo(HaveOccurred())

			Expect(savedRun.PlaybookID).To(Equal(testPlaybook.ID), "run should have been created for the correct playbook")
			Expect(*savedRun.CreatedBy).To(Equal(dummy.JohnDoe.ID), "run should have been created by the authenticated person")
		})

		ginkgo.It("should have correctly used config & component fields from parameters", func() {

			waitFor(&models.PlaybookRun{
				ID: uuid.MustParse(runResp.RunID),
			})

			f, err := os.ReadFile(tempFile)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(f)).To(Equal(fmt.Sprintf("%s\n%s\n", dummy.EKSCluster.ConfigClass, dummy.Logistics.Name)))
		})
	})

	var _ = ginkgo.Describe("Test Playbook action filters", ginkgo.Ordered, func() {
		var (
			spec     v1.PlaybookSpec
			playbook models.Playbook
			run      *models.PlaybookRun
			dataFile = "/tmp/action-filter-test.txt"
			logFile  = "/tmp/action-filter-test-log.txt"
		)

		ginkgo.BeforeAll(func() {
			playbook, spec = createPlaybook("action-filter")
		})

		ginkgo.It("should execute the playbook", func() {
			run = runPlaybook(playbook, RunParams{
				ConfigID: dummy.EKSCluster.ID,
				Params: map[string]string{
					"path":     dataFile,
					"log_path": logFile,
				},
			})
		})

		ginkgo.It("should have correctly ran some and skipped some of the actions", func() {
			var actions []models.PlaybookRunAction
			err := DefaultContext.DB().Where("playbook_run_id = ?", run.ID).Find(&actions).Error
			Expect(err).NotTo(HaveOccurred())

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

		ginkgo.It("should have populated the files correctly", func() {
			data, err := os.ReadFile(dataFile)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(data)).To(Equal(models.ConfigClassCluster))

			logs, err := os.ReadFile(logFile)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(logs)).To(Equal("File creation succeeded\nCommand failed\n==end==\n"))
		})
	})

	var _ = ginkgo.Describe("Test Accessing previous results in actions", ginkgo.Ordered, func() {
		var (
			spec     v1.PlaybookSpec
			playbook models.Playbook
			run      *models.PlaybookRun
			dataFile = "/tmp/access-previous-result.txt"
		)

		ginkgo.BeforeAll(func() {
			playbook, spec = createPlaybook("action-last-result")
		})

		ginkgo.It("should store playbook run via API", func() {
			run = runPlaybook(playbook, RunParams{
				ConfigID: dummy.EKSCluster.ID,
				Params: map[string]string{
					"path": dataFile,
				},
			})
		})

		ginkgo.It("should have correctly ran some and skipped some of the actions", func() {
			var actions []models.PlaybookRunAction
			err := DefaultContext.DB().Where("playbook_run_id = ?", run.ID).Find(&actions).Error
			Expect(err).NotTo(HaveOccurred())

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
					ginkgo.Fail("Unexpected action: " + action.Name)
					continue
				}

				Expect(action.Status).To(Equal(expected), action.Name)
			}
		})

		ginkgo.It("should have populated the files correctly", func() {
			data, err := os.ReadFile(dataFile)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(data)).To(Equal("HIGH\n20"))
		})
	})

	var _ = ginkgo.Describe("Test Playbook runners", ginkgo.Ordered, func() {
		var (
			spec     v1.PlaybookSpec
			playbook models.Playbook
			run      *models.PlaybookRun
			err      error

			actionOnAgent  models.PlaybookRunAction
			upstreamConfig upstream.UpstreamConfig
			agentName      = "aws-agent"
			awsAgent       models.Agent

			agentContext *context.Context
			agentDBDrop  func()
		)

		ginkgo.BeforeAll(func() {
			playbook, spec = createPlaybook("agent-runner")

			// Setup agent
			agentContext, agentDBDrop, err = setup.NewDB(DefaultContext, "aws")
			Expect(err).NotTo(HaveOccurred())

			upstreamConfig = upstream.UpstreamConfig{
				AgentName: "aws",
				Host:      fmt.Sprintf("http://localhost:%d", echoServerPort),
				Username:  agentName,
				Password:  "dummy",
			}

			// save the agent to the db
			agentPerson := models.Person{Name: agentName}
			err = DefaultContext.DB().Create(&agentPerson).Error
			Expect(err).NotTo(HaveOccurred())

			awsAgent = models.Agent{Name: "aws", PersonID: &agentPerson.ID}
			err = DefaultContext.DB().Create(&awsAgent).Error
			Expect(err).NotTo(HaveOccurred())
		})

		ginkgo.AfterAll(func() {
			if agentDBDrop != nil {
				agentDBDrop()
			}
		})

		ginkgo.It("should execute the playbook", func() {
			run = runPlaybook(playbook, RunParams{
				ConfigID: dummy.KubernetesNodeA.ID,
			}, models.PlaybookRunStatusWaiting)
		})

		ginkgo.It("should pull the action from the upstream", func() {
			pulled, err := PullPlaybookAction(*agentContext, upstreamConfig)
			Expect(err).NotTo(HaveOccurred())
			Expect(pulled).To(BeTrue())

			err = agentContext.DB().Where("name = ? ", spec.Actions[1].Name).First(&actionOnAgent).Error
			Expect(err).To(BeNil())

			Expect(actionOnAgent.Status).To(Equal(models.PlaybookActionStatusScheduled))
		})

		ginkgo.It("the upstream should also have the same action assigned to our agent", func() {
			var actionOnUpstream models.PlaybookRunAction
			err := agentContext.DB().Where("name = ? ", spec.Actions[1].Name).First(&actionOnUpstream).Error
			Expect(err).To(BeNil())

			Expect(actionOnAgent.ID.String()).To(Equal(actionOnUpstream.ID.String()))
			Expect(actionOnUpstream.Status).To(Equal(models.PlaybookActionStatusScheduled))
			Expect(actionOnUpstream.AgentID).To(Not(BeNil()))
			Expect(actionOnUpstream.AgentID.String()).To(Equal(awsAgent.ID.String()))
		})

		ginkgo.It("should run the pulled action on the agent", func() {
			err := StartPlaybookConsumers(*agentContext)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.PlaybookActionStatus {
				// Manually publish a pg_notify event because for some reason the embedded db isn't realiable
				err := agentContext.DB().Exec("NOTIFY playbook_action_updates").Error
				Expect(err).NotTo(HaveOccurred())

				var action models.PlaybookRunAction
				err = agentContext.DB().Select("status").Where("id = ? ", actionOnAgent.ID).First(&action).Error
				Expect(err).To(BeNil())

				return action.Status
			}, "10s", "1s").Should(Equal(models.PlaybookActionStatusCompleted))
		})

		ginkgo.It("should push the action result to the upstream", func() {
			pushed, err := PushPlaybookActions(*agentContext, upstreamConfig, 10)
			Expect(err).NotTo(HaveOccurred())
			Expect(pushed).To(Equal(1))
		})

		// This test can fail if we update the openAPI schema but the change
		// is not pushed to duty yet
		// Since duty syncs schema changes from this repo, this becomes a deadlock situation
		// Workaround for now is to update openapi schemas in duty manually and then bump duty
		ginkgo.It("should ensure that the playbook ran to completion", func() {
			waitFor(run, models.PlaybookRunStatusCompleted)
		})

		ginkgo.It("should ensure that the playbook ran correctly", func() {
			var actions []models.PlaybookRunAction
			err = DefaultContext.DB().Where("playbook_run_id = ? ", run.ID).Find(&actions).Error
			Expect(err).NotTo(HaveOccurred())

			Expect(len(actions)).To(Equal(2))
			for i := range actions {
				Expect(actions[i].Status).To(Equal(models.PlaybookActionStatusCompleted))
				Expect(actions[i].Result["stdout"]).To(Equal(dummy.KubernetesNodeA.ConfigClass))
			}
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
		ginkgo.It(test.description, func() {
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
	run, err := validateAndSavePlaybookRun(DefaultContext, &playbook, params)
	Expect(err).NotTo(HaveOccurred())
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
		if savedRun != nil {
			return savedRun.Status
		}
		events.ConsumeAll(DefaultContext)
		_, _ = RunConsumer(DefaultContext)

		return models.PlaybookRunStatus("Unknown")
	}).WithTimeout(15 * time.Second).WithPolling(time.Second).Should(BeElementOf(s))

	return savedRun
}
