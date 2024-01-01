package playbook

import (
	"encoding/json"
	"fmt"
	netHTTP "net/http"
	"os"
	"time"

	"github.com/flanksource/commons/http"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/events"
	"github.com/flanksource/postq"
	"github.com/flanksource/postq/pg"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm/clause"
)

var _ = ginkgo.Describe("Playbook runner", ginkgo.Ordered, func() {
	var (
		configPlaybook    models.Playbook
		checkPlaybook     models.Playbook
		componentPlaybook models.Playbook
		runResp           RunResponse

		pgNotifyChannel chan string
		ec              *postq.PGConsumer
	)

	ginkgo.BeforeAll(func() {
		pgNotifyChannel = make(chan string)

		var err error
		ec, err = postq.NewPGConsumer(EventConsumer, &postq.ConsumerOption{
			NumConsumers: 5,
			Timeout:      time.Second * 2,
			ErrorHandler: func(ctx postq.Context, err error) bool {
				logger.Errorf("Error in queue consumer: %s", err)
				return true
			},
		})
		Expect(err).NotTo(HaveOccurred())

		go events.StartConsumers(DefaultContext)

		playbookSpec := v1.PlaybookSpec{
			Description: "write config name to file",
			Parameters: []v1.PlaybookParameter{
				{Name: "path", Label: "path of the file"},
			},
			Configs: []v1.PlaybookResourceFilter{
				{Type: *dummy.EKSCluster.Type, Tags: map[string]string{"environment": "production"}},
			},
			Approval: &v1.PlaybookApproval{
				Type: v1.PlaybookApprovalTypeAny, // We have two approvers (John Doe & John Wick) and just a single approval is sufficient
				Approvers: v1.PlaybookApprovers{
					People: []string{
						dummy.JohnDoe.Email,
						dummy.JohnWick.Email,
					},
				},
			},
			Actions: []v1.PlaybookAction{
				{
					Name:    "write config id to a file",
					Timeout: "1s",
					Exec: &v1.ExecAction{
						Script: "echo id={{.config.id}}  path={{.params.path}} && printf {{.config.id}} > {{.params.path}}",
					},
				},
				{
					Name:    "append config class to the same file ",
					Timeout: "2s",
					Exec: &v1.ExecAction{
						Script: "echo class={{.config.config_class}} path={{.params.path}} && printf {{.config.config_class}} >> {{.params.path}}",
					},
				},
			},
		}

		spec, err := json.Marshal(playbookSpec)
		Expect(err).NotTo(HaveOccurred())

		configPlaybook = models.Playbook{
			Name:   "config name saver",
			Spec:   spec,
			Source: models.SourceConfigFile,
		}

		err = DefaultContext.DB().Clauses(clause.Returning{}).Create(&configPlaybook).Error
		Expect(err).NotTo(HaveOccurred())

		checkPlaybookSpec := v1.PlaybookSpec{
			Description: "write check name to file",
			Checks: []v1.PlaybookResourceFilter{
				{Type: dummy.LogisticsAPIHealthHTTPCheck.Type},
			},
			Actions: []v1.PlaybookAction{
				{
					Name: "write check name to a file",
					Exec: &v1.ExecAction{
						Script: "printf {{.check.id}} > /tmp/{{.check.id}}.txt",
					},
				},
			},
		}

		spec, err = json.Marshal(checkPlaybookSpec)
		Expect(err).NotTo(HaveOccurred())

		checkPlaybook = models.Playbook{
			Name:   "check name saver",
			Spec:   spec,
			Source: models.SourceConfigFile,
		}

		err = DefaultContext.DB().Clauses(clause.Returning{}).Create(&checkPlaybook).Error
		Expect(err).NotTo(HaveOccurred())

		componentPlaybookSpec := v1.PlaybookSpec{
			Description: "write component name to file",
			Components: []v1.PlaybookResourceFilter{
				{Type: dummy.Logistics.Type, Tags: map[string]string{"telemetry": "enabled"}},
			},
			Actions: []v1.PlaybookAction{
				{
					Name: "write component name to a file",
					Exec: &v1.ExecAction{
						Script: "echo name={{.component.name}}  && printf {{.component.name}} > /tmp/{{.component.name}}.txt",
					},
				},
			},
		}

		spec, err = json.Marshal(componentPlaybookSpec)
		Expect(err).NotTo(HaveOccurred())

		componentPlaybook = models.Playbook{
			Name:   "component name saver",
			Spec:   spec,
			Source: models.SourceConfigFile,
		}

		err = DefaultContext.DB().Clauses(clause.Returning{}).Create(&componentPlaybook).Error
		Expect(err).NotTo(HaveOccurred())
	})

	ginkgo.It("Should fetch the suitable playbook for checks", func() {
		playbooks, err := ListPlaybooksForCheck(DefaultContext, dummy.LogisticsAPIHealthHTTPCheck.ID.String())
		Expect(err).NotTo(HaveOccurred())
		Expect(len(playbooks)).To(Equal(1))
		Expect(playbooks[0].ID).To(Equal(checkPlaybook.ID))

		playbooks, err = ListPlaybooksForCheck(DefaultContext, dummy.LogisticsDBCheck.ID.String())
		Expect(err).NotTo(HaveOccurred())
		Expect(len(playbooks)).To(Equal(0))
	})

	ginkgo.It("Should fetch the suitable playbook for components", func() {
		playbooks, err := ListPlaybooksForComponent(DefaultContext, dummy.Logistics.ID.String())
		Expect(err).NotTo(HaveOccurred())
		Expect(len(playbooks)).To(Equal(1))
		Expect(playbooks[0].ID).To(Equal(componentPlaybook.ID))

		playbooks, err = ListPlaybooksForComponent(DefaultContext, dummy.LogisticsUI.ID.String())
		Expect(err).NotTo(HaveOccurred())
		Expect(len(playbooks)).To(Equal(0))
	})

	ginkgo.It("Should fetch the suitable playbook for configs", func() {
		playbooks, err := ListPlaybooksForConfig(DefaultContext, dummy.EKSCluster.ID.String())
		Expect(err).NotTo(HaveOccurred())
		Expect(len(playbooks)).To(Equal(1))
		Expect(playbooks[0].ID).To(Equal(configPlaybook.ID))

		playbooks, err = ListPlaybooksForConfig(DefaultContext, dummy.KubernetesCluster.ID.String())
		Expect(err).NotTo(HaveOccurred())
		Expect(len(playbooks)).To(Equal(0))
	})

	ginkgo.It("should store playbook run via API", func() {
		go pg.Listen(DefaultContext, "playbook_run_updates", pgNotifyChannel)
		go ec.Listen(DefaultContext, pgNotifyChannel)

		run := RunParams{
			ID:       configPlaybook.ID,
			ConfigID: dummy.EKSCluster.ID,
			Params: map[string]string{
				"path": tempPath,
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
		Expect(savedRun.Status).To(Equal(models.PlaybookRunStatusPending), "run should be in pending status because it has approvers")
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
		}, "3s").Should(Equal(models.PlaybookRunStatusCompleted))
	})

	ginkgo.It("should execute playbook", func() {
		var updatedRun models.PlaybookRun

		// Wait until all the runs is marked as completed
		var attempts int
		for {
			time.Sleep(time.Millisecond * 100)

			err := DefaultContext.DB().Where("id = ? ", runResp.RunID).First(&updatedRun).Error
			Expect(err).NotTo(HaveOccurred())

			if updatedRun.Status == models.PlaybookRunStatusCompleted {
				break
			}

			attempts += 1
			if attempts > 50 { // wait for 5 seconds
				ginkgo.Fail(fmt.Sprintf("Timed out waiting for run to complete. Run status: %s", updatedRun.Status))
			}
		}

		var runActions []models.PlaybookRunAction
		err := DefaultContext.DB().Where("playbook_run_id = ?", updatedRun.ID).Find(&runActions).Error
		Expect(err).NotTo(HaveOccurred())

		d, _ := json.Marshal(runActions)
		Expect(len(runActions)).To(Equal(2))
		fmt.Println(string(d))

		f, err := os.ReadFile(tempPath)
		Expect(err).NotTo(HaveOccurred())

		Expect(string(f)).To(Equal(fmt.Sprintf("%s%s", dummy.EKSCluster.ID, dummy.EKSCluster.ConfigClass)))
	})
})
