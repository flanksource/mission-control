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
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("Playbook runner", ginkgo.Ordered, func() {
	playbookSpec := v1.PlaybookSpec{
		Description: "write config name to file",
		Parameters: []v1.PlaybookParameter{
			{Name: "path", Label: "path of the file"},
		},
		Actions: []v1.PlaybookAction{
			{
				Name: "write config name to a file",
				Exec: &v1.ExecAction{
					Script: "printf {{.config.config_class}} > {{.params.path}}",
				},
			},
		},
	}

	var (
		playbook models.Playbook
		runResp  RunResponse
	)
	ginkgo.It("should create a new playbook", func() {
		spec, err := json.Marshal(playbookSpec)
		Expect(err).NotTo(HaveOccurred())

		playbook = models.Playbook{
			Name: "config name saver",
			Spec: spec,
		}

		err = testDB.Create(&playbook).Error
		Expect(err).NotTo(HaveOccurred())
	})

	ginkgo.It("should store dummy data", func() {
		err := dummy.PopulateDBWithDummyModels(testDB)
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
		req.SetBasicAuth("admin@local", "admin")

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

		Expect(savedRun.PlaybookID).To(Equal(playbook.ID))
	})

	ginkgo.It("should execute playbook", func() {
		consumer := NewQueueConsumer(testDB, testDBPool)

		ctx := api.NewContext(testDB, nil)
		err := consumer.consumeAll(ctx)
		Expect(err).NotTo(HaveOccurred())

		// Wait for the action to complete
		time.Sleep(time.Second * 5)

		var updatedRun models.PlaybookRun
		err = testDB.Where("id = ? ", runResp.RunID).First(&updatedRun).Error
		Expect(err).NotTo(HaveOccurred())

		Expect(updatedRun.Status).To(Equal(models.PlaybookRunStatusCompleted))

		f, err := os.ReadFile(tempPath)
		Expect(err).NotTo(HaveOccurred())

		Expect(string(f)).To(Equal(dummy.EKSCluster.ConfigClass))
	})
})
