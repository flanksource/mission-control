package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"time"

	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/sdk"
)

var _ = ginkgo.Describe("playbook CLI helpers", func() {
	var savedParamFile string
	var savedConfigID string
	var savedComponentID string
	var savedCheckID string
	var savedPollInterval time.Duration

	ginkgo.BeforeEach(func() {
		savedParamFile = paramFile
		savedConfigID = playbookConfigID
		savedComponentID = playbookComponentID
		savedCheckID = playbookCheckID
		savedPollInterval = playbookPollInterval
	})

	ginkgo.AfterEach(func() {
		paramFile = savedParamFile
		playbookConfigID = savedConfigID
		playbookComponentID = savedComponentID
		playbookCheckID = savedCheckID
		playbookPollInterval = savedPollInterval
	})

	ginkgo.It("resolves playbook refs by id, namespace/name, and unambiguous name", func() {
		firstID := uuid.New()
		secondID := uuid.New()
		playbooks := []api.PlaybookListItem{
			{ID: firstID, Namespace: "default", Name: "restart"},
			{ID: secondID, Namespace: "ops", Name: "diagnose"},
		}

		byID, err := resolvePlaybookRef(playbooks, firstID.String(), "default")
		Expect(err).ToNot(HaveOccurred())
		Expect(byID.ID).To(Equal(firstID))

		byQualifiedName, err := resolvePlaybookRef(playbooks, "ops/diagnose", "default")
		Expect(err).ToNot(HaveOccurred())
		Expect(byQualifiedName.ID).To(Equal(secondID))

		byName, err := resolvePlaybookRef(playbooks, "diagnose", "default")
		Expect(err).ToNot(HaveOccurred())
		Expect(byName.ID).To(Equal(secondID))
	})

	ginkgo.It("builds remote run params from files, flags, and key value args", func() {
		configID := uuid.New()
		playbookID := uuid.New()
		file := ginkgo.GinkgoT().TempDir() + "/params.yaml"
		Expect(os.WriteFile(file, []byte("name: api\n"), 0600)).To(Succeed())
		paramFile = file
		playbookConfigID = configID.String()

		params, err := buildRemoteRunParams(playbookID, []string{"region=eu-west-1"})
		Expect(err).ToNot(HaveOccurred())
		Expect(params.ID).To(Equal(playbookID))
		Expect(params.ConfigID).To(Equal(&configID))
		Expect(params.Params).To(HaveKeyWithValue("name", "api"))
		Expect(params.Params).To(HaveKeyWithValue("region", "eu-west-1"))
	})

	ginkgo.It("streams status transitions to stderr while waiting", func() {
		runID := uuid.New()
		actionID := uuid.New()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.URL.Path).To(Equal("/playbook/run/" + runID.String() + "/status"))
			w.Header().Set("Content-Type", "application/json")
			Expect(json.NewEncoder(w).Encode(sdk.PlaybookSummary{
				Run: models.PlaybookRun{
					ID:     runID,
					Status: models.PlaybookRunStatusCompleted,
				},
				Actions: []models.PlaybookRunAction{{
					ID:     actionID,
					Name:   "echo",
					Status: models.PlaybookActionStatusCompleted,
				}},
			})).To(Succeed())
		}))
		defer server.Close()

		var stderr bytes.Buffer
		summary, err := waitForRemotePlaybookRun(&stderr, sdk.New(server.URL, "fake-token"), runID.String())
		Expect(err).ToNot(HaveOccurred())
		Expect(summary.Run.Status).To(Equal(models.PlaybookRunStatusCompleted))
		Expect(stderr.String()).To(ContainSubstring("run " + runID.String() + " status=completed"))
		Expect(stderr.String()).To(ContainSubstring("action echo status=completed"))
	})

	ginkgo.It("prints playbook lists as a compact table by default", func() {
		id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
		var stdout bytes.Buffer

		err := savePlaybookList(&stdout, []api.PlaybookListItem{{
			ID:        id,
			Category:  "Kubernetes",
			Namespace: "monitoring",
			Name:      "restart-pod",
		}}, "", false)

		Expect(err).ToNot(HaveOccurred())
		Expect(stdout.String()).To(ContainSubstring("CATEGORY"))
		Expect(stdout.String()).To(ContainSubstring("NAMESPACE"))
		Expect(stdout.String()).To(ContainSubstring("NAME"))
		Expect(stdout.String()).To(ContainSubstring("UUID"))
		Expect(stdout.String()).To(ContainSubstring("Kubernetes"))
		Expect(stdout.String()).To(ContainSubstring("monitoring"))
		Expect(stdout.String()).To(ContainSubstring("restart-pod"))
		Expect(stdout.String()).To(ContainSubstring(id.String()))
		Expect(stdout.String()).ToNot(ContainSubstring("description"))
	})

	ginkgo.It("prints full playbook list JSON when requested", func() {
		id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
		var stdout bytes.Buffer

		err := savePlaybookList(&stdout, []api.PlaybookListItem{{
			ID:          id,
			Category:    "Kubernetes",
			Namespace:   "monitoring",
			Name:        "restart-pod",
			Description: "Restarts a pod",
		}}, "", true)

		Expect(err).ToNot(HaveOccurred())
		Expect(stdout.String()).To(ContainSubstring(`"id": "` + id.String() + `"`))
		Expect(stdout.String()).To(ContainSubstring(`"description": "Restarts a pod"`))
	})
})
