package playbook_test

import (
	gocontext "context"
	"encoding/json"
	"fmt"
	netHTTP "net/http"
	"os"
	"time"

	"github.com/flanksource/commons/http"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/fixtures/dummy"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/upstream"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/events"
	"github.com/flanksource/incident-commander/playbook"
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
		runResp           playbook.RunResponse

		pgNotifyChannel chan string
		ec              *postq.PGConsumer
	)

	ginkgo.It("should store dummy data", func() {
		dataset := dummy.GetStaticDummyData()
		err := dataset.Populate(testDB)
		Expect(err).NotTo(HaveOccurred())
	})

	ginkgo.It("start the queue consumer in background", func() {
		pgNotifyChannel = make(chan string)

		ctx := context.NewContext(gocontext.Background()).WithDB(testDB, testDBPool)

		var err error
		ec, err = postq.NewPGConsumer(playbook.EventConsumer, &postq.ConsumerOption{
			NumConsumers: 5,
			Timeout:      time.Second * 2,
			ErrorHandler: func(err error) bool {
				logger.Errorf("Error in queue consumer: %s", err)
				return true
			},
		})
		Expect(err).NotTo(HaveOccurred())

		go events.StartConsumers(ctx, upstream.UpstreamConfig{})
	})

	ginkgo.It("should create a new playbook", func() {
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
						Script: "printf {{.config.id}} > {{.params.path}}",
					},
				},
				{
					Name:    "append config class to the same file ",
					Timeout: "2s",
					Exec: &v1.ExecAction{
						Script: "printf {{.config.config_class}} >> {{.params.path}}",
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

		err = testDB.Clauses(clause.Returning{}).Create(&configPlaybook).Error
		Expect(err).NotTo(HaveOccurred())
	})

	ginkgo.It("should save other playbooks so we can test listing of playbooks for checks, canaries & components", func() {
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

		spec, err := json.Marshal(checkPlaybookSpec)
		Expect(err).NotTo(HaveOccurred())

		checkPlaybook = models.Playbook{
			Name:   "check name saver",
			Spec:   spec,
			Source: models.SourceConfigFile,
		}

		err = testDB.Clauses(clause.Returning{}).Create(&checkPlaybook).Error
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
						Script: "printf {{.component.name}} > /tmp/{{.component.name}}.txt",
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

		err = testDB.Clauses(clause.Returning{}).Create(&componentPlaybook).Error
		Expect(err).NotTo(HaveOccurred())
	})

	ginkgo.It("Should fetch the suitable playbook for checks", func() {
		ctx := context.NewContext(gocontext.Background()).WithDB(testDB, testDBPool)
		playbooks, err := playbook.ListPlaybooksForCheck(ctx, dummy.LogisticsAPIHealthHTTPCheck.ID.String())
		Expect(err).NotTo(HaveOccurred())
		Expect(len(playbooks)).To(Equal(1))
		Expect(playbooks[0].ID).To(Equal(checkPlaybook.ID))

		playbooks, err = playbook.ListPlaybooksForCheck(ctx, dummy.LogisticsDBCheck.ID.String())
		Expect(err).NotTo(HaveOccurred())
		Expect(len(playbooks)).To(Equal(0))
	})

	ginkgo.It("Should fetch the suitable playbook for components", func() {
		ctx := context.NewContext(gocontext.Background()).WithDB(testDB, testDBPool)
		playbooks, err := playbook.ListPlaybooksForComponent(ctx, dummy.Logistics.ID.String())
		Expect(err).NotTo(HaveOccurred())
		Expect(len(playbooks)).To(Equal(1))
		Expect(playbooks[0].ID).To(Equal(componentPlaybook.ID))

		playbooks, err = playbook.ListPlaybooksForComponent(ctx, dummy.LogisticsUI.ID.String())
		Expect(err).NotTo(HaveOccurred())
		Expect(len(playbooks)).To(Equal(0))
	})

	ginkgo.It("Should fetch the suitable playbook for configs", func() {
		ctx := context.NewContext(gocontext.Background()).WithDB(testDB, testDBPool)
		playbooks, err := playbook.ListPlaybooksForConfig(ctx, dummy.EKSCluster.ID.String())
		Expect(err).NotTo(HaveOccurred())
		Expect(len(playbooks)).To(Equal(1))
		Expect(playbooks[0].ID).To(Equal(configPlaybook.ID))

		playbooks, err = playbook.ListPlaybooksForConfig(ctx, dummy.KubernetesCluster.ID.String())
		Expect(err).NotTo(HaveOccurred())
		Expect(len(playbooks)).To(Equal(0))
	})

	ginkgo.It("should store playbook run via API", func() {
		ctx := context.NewContext(gocontext.Background()).WithDB(testDB, testDBPool)

		go pg.Listen(ctx, "playbook_run_updates", pgNotifyChannel)
		go ec.Listen(ctx, pgNotifyChannel)

		run := playbook.RunParams{
			ID:       configPlaybook.ID,
			ConfigID: dummy.EKSCluster.ID,
			Params: map[string]string{
				"path": tempPath,
			},
		}

		httpClient := http.NewClient().Auth(dummy.JohnDoe.Name, "admin").BaseURL(fmt.Sprintf("http://localhost:%d/playbook", echoServerPort))
		resp, err := httpClient.R(ctx).Header("Content-Type", "application/json").Post("/run", run)
		Expect(err).NotTo(HaveOccurred())

		Expect(resp.StatusCode).To(Equal(netHTTP.StatusCreated))

		err = json.NewDecoder(resp.Body).Decode(&runResp)
		Expect(err).NotTo(HaveOccurred())

		var savedRun models.PlaybookRun
		err = testDB.Where("id = ? ", runResp.RunID).First(&savedRun).Error
		Expect(err).NotTo(HaveOccurred())

		Expect(savedRun.PlaybookID).To(Equal(configPlaybook.ID), "run should have been created for the correct playbook")
		Expect(savedRun.Status).To(Equal(models.PlaybookRunStatusPending), "run should be in pending status because it has approvers")
		Expect(*savedRun.CreatedBy).To(Equal(dummy.JohnDoe.ID), "run should have been created by the authenticated person")
	})

	ginkgo.It("should have auto approved & scheduled the playbook run", func() {
		var attempts int
		for {
			time.Sleep(time.Millisecond * 100)

			var savedRun models.PlaybookRun
			err := testDB.Where("id = ? ", runResp.RunID).First(&savedRun).Error
			Expect(err).NotTo(HaveOccurred())

			if savedRun.Status == models.PlaybookRunStatusScheduled || savedRun.Status == models.PlaybookRunStatusCompleted {
				break
			}

			attempts += 1
			if attempts > 50 { // wait for max 5 seconds
				ginkgo.Fail(fmt.Sprintf("Timed out waiting for run to be scheduled. Status = %s", savedRun.Status))
			}
		}
	})

	ginkgo.It("should execute playbook", func() {
		var updatedRun models.PlaybookRun

		// Wait until all the runs is marked as completed
		var attempts int
		for {
			time.Sleep(time.Millisecond * 100)

			err := testDB.Where("id = ? ", runResp.RunID).First(&updatedRun).Error
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
		err := testDB.Where("playbook_run_id = ?", updatedRun.ID).Find(&runActions).Error
		Expect(err).NotTo(HaveOccurred())

		Expect(len(runActions)).To(Equal(2))

		f, err := os.ReadFile(tempPath)
		Expect(err).NotTo(HaveOccurred())

		Expect(string(f)).To(Equal(fmt.Sprintf("%s%s", dummy.EKSCluster.ID, dummy.EKSCluster.ConfigClass)))
	})
})
