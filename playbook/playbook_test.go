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
	v1 "github.com/flanksource/incident-commander/api/v1"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("Playbook runner", ginkgo.Ordered, func() {
	var (
		playbook models.Playbook
		runResp  RunResponse
		consumer *queueConsumer
	)

	ginkgo.It("should store dummy data", func() {
		dataset := dummy.GetStaticDummyData()
		err := dataset.Populate(testDB)
		Expect(err).NotTo(HaveOccurred())
	})

	ginkgo.It("start the queue consumer in background", func() {
		consumer = NewQueueConsumer(testDB, testDBPool)
		go func() {
			err := consumer.Listen()
			Expect(err).NotTo(HaveOccurred())
		}()
	})

	ginkgo.It("should create a new playbook", func() {
		playbookSpec := v1.PlaybookSpec{
			Description: "write config name to file",
			Parameters: []v1.PlaybookParameter{
				{Name: "path", Label: "path of the file"},
			},
			Approval: &v1.PlaybookApproval{
				Type: v1.PlaybookApprovalTypeAny, // We have two approvers (John Doe & John Wick) and just a single approval is sufficient
				Approvers: v1.PlaybookApprovers{
					People: []string{
						dummy.JohnDoe.ID.String(),
						dummy.JohnWick.ID.String(),
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

		playbook = models.Playbook{
			Name:   "config name saver",
			Spec:   spec,
			Source: models.SourceConfigFile,
		}

		err = testDB.Create(&playbook).Error
		Expect(err).NotTo(HaveOccurred())
	})

	ginkgo.It("should store playbook run via API", func() {
		run := RunParams{
			ID:       playbook.ID,
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

		Expect(savedRun.PlaybookID).To(Equal(playbook.ID), "run should have been created for the correct playbook")
		Expect(savedRun.Status).To(Equal(models.PlaybookRunStatusPending), "run should be in pending status because it has approvers")
		Expect(*savedRun.CreatedBy).To(Equal(dummy.JohnDoe.ID), "run should have been created by the authenticated person")
	})

	ginkgo.It("should approve the playbook run via API", func() {
		req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://localhost:%d/playbook/run/approve/%s/%s", echoServerPort, playbook.ID.String(), runResp.RunID), nil)
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
			if attempts > 20 { // wait for 2 seconds
				ginkgo.Fail("Timed out waiting for run to complete")
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
