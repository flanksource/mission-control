package playbook

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/flanksource/duty/fixtures/dummy"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/events"
	"github.com/flanksource/incident-commander/events/eventconsumer"
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
	)

	ginkgo.It("should store dummy data", func() {
		dataset := dummy.GetStaticDummyData()
		err := dataset.Populate(testDB)
		Expect(err).NotTo(HaveOccurred())
	})

	ginkgo.It("start the queue consumer in background", func() {
		go eventconsumer.New(testDB, testDBPool, "playbook_run_updates", EventConsumer).
			WithNumConsumers(5).
			WithNotifyTimeout(time.Second * 2).
			Listen()

		go events.StartConsumers(testDB, testDBPool, events.Config{})
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
					Name: "write config id to a file",
					Exec: &v1.ExecAction{
						Script: "printf {{.config.id}} > {{.params.path}}",
					},
				},
				{
					Name: "append config class to the same file ",
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

	ginkgo.It("should save other playbooks to test listing of playbooks for checks & components", func() {
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
		ctx := api.NewContext(testDB, nil)
		playbooks, err := ListPlaybooksForCheck(ctx, dummy.LogisticsAPIHealthHTTPCheck.ID.String())
		Expect(err).NotTo(HaveOccurred())
		Expect(len(playbooks)).To(Equal(1))
		Expect(playbooks).To(Equal([]models.Playbook{checkPlaybook}))

		playbooks, err = ListPlaybooksForCheck(ctx, dummy.LogisticsDBCheck.ID.String())
		Expect(err).NotTo(HaveOccurred())
		Expect(len(playbooks)).To(Equal(0))
	})

	ginkgo.It("Should fetch the suitable playbook for components", func() {
		ctx := api.NewContext(testDB, nil)
		playbooks, err := ListPlaybooksForComponent(ctx, dummy.Logistics.ID.String())
		Expect(err).NotTo(HaveOccurred())
		Expect(len(playbooks)).To(Equal(1))
		Expect(playbooks).To(Equal([]models.Playbook{componentPlaybook}))

		playbooks, err = ListPlaybooksForComponent(ctx, dummy.LogisticsUI.ID.String())
		Expect(err).NotTo(HaveOccurred())
		Expect(len(playbooks)).To(Equal(0))
	})

	ginkgo.It("Should fetch the suitable playbook for configs", func() {
		ctx := api.NewContext(testDB, nil)
		playbooks, err := ListPlaybooksForConfig(ctx, dummy.EKSCluster.ID.String())
		Expect(err).NotTo(HaveOccurred())
		Expect(len(playbooks)).To(Equal(1))
		Expect(playbooks).To(Equal([]models.Playbook{configPlaybook}))

		playbooks, err = ListPlaybooksForConfig(ctx, dummy.KubernetesCluster.ID.String())
		Expect(err).NotTo(HaveOccurred())
		Expect(len(playbooks)).To(Equal(0))
	})

	ginkgo.It("should store playbook run via API", func() {
		run := RunParams{
			ID:       configPlaybook.ID,
			ConfigID: dummy.EKSCluster.ID,
			Params: map[string]string{
				"path": tempPath,
			},
		}

		bodyJSON, err := json.Marshal(run)
		Expect(err).NotTo(HaveOccurred())

		req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://localhost:%d/playbook/run", echoServerPort), bytes.NewBuffer(bodyJSON))
		Expect(err).NotTo(HaveOccurred())

		req.Header.Set("Content-Type", "application/json; charset=UTF-8")
		req.SetBasicAuth(dummy.JohnDoe.Name, "admin")

		client := http.Client{}
		resp, err := client.Do(req)
		Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			b, err := io.ReadAll(resp.Body)
			Expect(err).NotTo(HaveOccurred())

			fmt.Println(string(b))
		}

		Expect(resp.StatusCode).To(Equal(http.StatusCreated))

		err = json.NewDecoder(resp.Body).Decode(&runResp)
		Expect(err).NotTo(HaveOccurred())

		var savedRun models.PlaybookRun
		err = testDB.Where("id = ? ", runResp.RunID).First(&savedRun).Error
		Expect(err).NotTo(HaveOccurred())

		Expect(savedRun.PlaybookID).To(Equal(configPlaybook.ID), "run should have been created for the correct playbook")
		Expect(savedRun.Status).To(Equal(models.PlaybookRunStatusPending), "run should be in pending status because it has approvers")
		Expect(*savedRun.CreatedBy).To(Equal(dummy.JohnDoe.ID), "run should have been created by the authenticated person")
	})

	ginkgo.It("should approve the playbook run via API", func() {
		req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://localhost:%d/playbook/run/approve/%s/%s", echoServerPort, configPlaybook.ID.String(), runResp.RunID), nil)
		Expect(err).NotTo(HaveOccurred())

		req.Header.Set("Content-Type", "application/json; charset=UTF-8")
		req.SetBasicAuth(dummy.JohnWick.Name, "admin") // approve John Wick (who is an approver but not a creator of the playbook)

		client := http.Client{}
		resp, err := client.Do(req)
		Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			b, err := io.ReadAll(resp.Body)
			Expect(err).NotTo(HaveOccurred())

			fmt.Println(string(b))
		}

		Expect(resp.StatusCode).To(Equal(http.StatusOK))

		// Wait until all run is marked as scheduled
		var attempts int
		for {
			time.Sleep(time.Millisecond * 100)

			var savedRun models.PlaybookRun
			err = testDB.Where("id = ? ", runResp.RunID).First(&savedRun).Error
			Expect(err).NotTo(HaveOccurred())

			if savedRun.Status == models.PlaybookRunStatusScheduled || savedRun.Status == models.PlaybookRunStatusCompleted {
				break
			}

			attempts += 1
			if attempts > 20 { // wait for 2 seconds
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
