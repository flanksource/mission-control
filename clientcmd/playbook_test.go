package clientcmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"time"

	"github.com/flanksource/clicky"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/sdk"
)

var _ = ginkgo.Describe("playbook CLI helpers", func() {
	var savedParamFile string
	var savedConfigID string
	var savedComponentID string
	var savedCheckID string
	var savedPollInterval time.Duration
	var savedJSONLogs bool
	var savedFormatOptions clicky.FormatOptions

	ginkgo.BeforeEach(func() {
		savedParamFile = ParamFile
		savedConfigID = playbookConfigID
		savedComponentID = playbookComponentID
		savedCheckID = playbookCheckID
		savedPollInterval = playbookPollInterval
		savedJSONLogs = clicky.Flags.JsonLogs
		savedFormatOptions = clicky.Flags.FormatOptions
		clicky.Flags.JsonLogs = false
		clicky.Flags.FormatOptions = clicky.FormatOptions{}
	})

	ginkgo.AfterEach(func() {
		ParamFile = savedParamFile
		playbookConfigID = savedConfigID
		playbookComponentID = savedComponentID
		playbookCheckID = savedCheckID
		playbookPollInterval = savedPollInterval
		clicky.Flags.JsonLogs = savedJSONLogs
		clicky.Flags.FormatOptions = savedFormatOptions
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
		ParamFile = file
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
		Expect(stderr.String()).To(ContainSubstring("run_id=" + runID.String()))
		Expect(stderr.String()).To(ContainSubstring("status=completed"))
		Expect(stderr.String()).To(ContainSubstring("type=playbook_run_status"))
		Expect(stderr.String()).To(ContainSubstring("action=echo"))
		Expect(stderr.String()).To(ContainSubstring("type=playbook_action_status"))
	})

	ginkgo.It("prints only the action result for playbook run summaries", func() {
		actionID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
		var stdout bytes.Buffer

		err := PrintPlaybookActionResults(&stdout, &sdk.PlaybookSummary{
			Playbook: models.Playbook{Namespace: "ops", Name: "diagnose"},
			Run:      models.PlaybookRun{ID: uuid.New(), Status: models.PlaybookRunStatusCompleted},
			Actions: []models.PlaybookRunAction{{
				ID:     actionID,
				Name:   "HTTP Request",
				Status: models.PlaybookActionStatusCompleted,
				Result: map[string]any{"code": 200, "content": "37.59.119.142"},
			}},
		})

		Expect(err).ToNot(HaveOccurred())
		Expect(stdout.String()).To(ContainSubstring("result:"))
		Expect(stdout.String()).To(ContainSubstring("code: 200"))
		Expect(stdout.String()).To(ContainSubstring("content: 37.59.119.142"))
		Expect(stdout.String()).ToNot(ContainSubstring("playbook"))
		Expect(stdout.String()).ToNot(ContainSubstring("actions"))
		Expect(stdout.String()).ToNot(ContainSubstring(actionID.String()))
	})

	ginkgo.It("prints action results as JSON when clicky JSON output is enabled", func() {
		clicky.Flags.FormatOptions.JSON = true
		var stdout bytes.Buffer

		err := PrintPlaybookActionResults(&stdout, &sdk.PlaybookSummary{
			Actions: []models.PlaybookRunAction{{
				Name:   "HTTP Request",
				Status: models.PlaybookActionStatusCompleted,
				Result: map[string]any{"code": 200},
			}},
		})

		Expect(err).ToNot(HaveOccurred())
		Expect(stdout.String()).To(ContainSubstring(`"result": {`))
		Expect(stdout.String()).To(ContainSubstring(`"code": 200`))
	})

	for _, tt := range []struct {
		name string
		opts clicky.FormatOptions
	}{
		{name: "CSV", opts: clicky.FormatOptions{CSV: true}},
		{name: "table", opts: clicky.FormatOptions{Table: true}},
	} {
		ginkgo.It("prints action results as "+tt.name+" with clicky", func() {
			clicky.Flags.FormatOptions = tt.opts
			var stdout bytes.Buffer

			err := PrintPlaybookActionResults(&stdout, &sdk.PlaybookSummary{
				Actions: []models.PlaybookRunAction{{
					Name:   "HTTP Request",
					Status: models.PlaybookActionStatusCompleted,
					Result: map[string]any{
						"code":        200,
						"content":     "37.59.119.142",
						"contentType": "text/plain",
					},
				}},
			})

			Expect(err).ToNot(HaveOccurred())
			Expect(stdout.String()).ToNot(BeEmpty())
		})
	}

	ginkgo.It("prints playbook lists as a compact table by default", func() {
		id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
		var stdout bytes.Buffer

		err := savePlaybookList(&stdout, []api.PlaybookListItem{{
			ID:        id,
			Category:  "Kubernetes",
			Namespace: "monitoring",
			Name:      "restart-pod",
		}}, false)

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
		}}, true)

		Expect(err).ToNot(HaveOccurred())
		Expect(stdout.String()).To(ContainSubstring(`"id": "` + id.String() + `"`))
		Expect(stdout.String()).To(ContainSubstring(`"description": "Restarts a pod"`))
	})

	ginkgo.It("requires config id for cached playbooks with config selectors", func() {
		id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
		spec := []byte(`{"configs":[{"types":["Kubernetes::Deployment"]}],"actions":[{"exec":{"script":"echo ok"}}]}`)
		cmd := newCachedPlaybookCommand(api.PlaybookListItem{
			ID:        id,
			Namespace: "mission-control",
			Name:      "kubernetes-update-image",
			Spec:      spec,
		}, "kubernetes-update-image")

		_, errOut, err := executeCommand(cmd)

		Expect(err).To(MatchError(ContainSubstring(`required flag(s) "config-id" not set`)))
		Expect(errOut).NotTo(ContainSubstring("Usage:"))
	})

	ginkgo.It("does not send templated defaults unless the cached playbook flag is changed", func() {
		param := v1.PlaybookParameter{
			Name:    "container",
			Type:    v1.PlaybookParameterTypeText,
			Default: `$( .config.config | jq ".spec.template.spec.containers[0].name" )`,
		}
		cmd := &cobra.Command{Use: "kubernetes-update-image"}
		value := string(param.Default)
		values := map[string]*string{"container": &value}
		cmd.Flags().StringVar(values["container"], "container", value, "")

		args, err := cachedPlaybookParamArgs(cmd, []v1.PlaybookParameter{param}, values)
		Expect(err).ToNot(HaveOccurred())
		Expect(args).To(BeEmpty())

		Expect(cmd.Flags().Set("container", "api")).To(Succeed())
		args, err = cachedPlaybookParamArgs(cmd, []v1.PlaybookParameter{param}, values)
		Expect(err).ToNot(HaveOccurred())
		Expect(args).To(Equal([]string{"container=api"}))
	})
})
