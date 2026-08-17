package clientcmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"time"

	"github.com/flanksource/clicky"
	"github.com/flanksource/incident-commander/clientapi"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"

	sdk "github.com/flanksource/incident-commander/sdk/client"
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
		playbooks := []clientapi.PlaybookListItem{
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

	ginkgo.It("does not stream status transitions by default while waiting", func() {
		runID := uuid.New()
		actionID := uuid.New()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.URL.Path).To(Equal("/playbook/run/" + runID.String() + "/status"))
			w.Header().Set("Content-Type", "application/json")
			Expect(json.NewEncoder(w).Encode(sdk.PlaybookSummary{
				Run: clientapi.PlaybookRun{
					ID:     runID,
					Status: clientapi.PlaybookRunStatusCompleted,
				},
				Actions: []clientapi.PlaybookRunAction{{
					ID:     actionID,
					Name:   "echo",
					Status: clientapi.PlaybookActionStatus("completed"),
				}},
			})).To(Succeed())
		}))
		defer server.Close()

		var stderr bytes.Buffer
		summary, err := waitForRemotePlaybookRun(&stderr, sdk.New(server.URL, "fake-token"), runID.String())
		Expect(err).ToNot(HaveOccurred())
		Expect(summary.Run.Status).To(Equal(clientapi.PlaybookRunStatusCompleted))
		Expect(stderr.String()).To(BeEmpty())
	})

	actionResults := []struct {
		name         string
		actionType   string
		result       map[string]any
		expectedType any
		expected     []string
	}{
		{
			name:       "SQL",
			actionType: "sql",
			result: map[string]any{
				"columns": []string{"name", "ready"},
				"rows":    []map[string]any{{"name": "api", "ready": true}},
			},
			expectedType: playbookSQLResult{},
			expected:     []string{"api", "true"},
		},
		{
			name:       "exec",
			actionType: "exec",
			result: map[string]any{
				"stdout":   "ok",
				"stderr":   "warning",
				"exitCode": 2,
				"path":     "/bin/sh",
				"args":     []string{"-c", "echo ok"},
				"extra":    map[string]any{"commit": "abc123"},
			},
			expectedType: playbookExecResult{},
			expected:     []string{"Stdout:", "ok", "Stderr:", "warning", "Exit Code: 2"},
		},
		{
			name:       "HTTP",
			actionType: "http",
			result: map[string]any{
				"code":    200,
				"headers": map[string]string{"Content-Type": "application/json"},
				"content": `{"ready":true}`,
			},
			expectedType: playbookHTTPResult{},
			expected:     []string{"Status: 200", "Content-Type: application/json", `{"ready":true}`},
		},
	}
	for _, tt := range actionResults {
		ginkgo.It("preserves "+tt.name+" action result formatting", func() {
			result := resolveActionResult(tt.actionType, tt.result)
			Expect(result).To(BeAssignableToTypeOf(tt.expectedType))
			formatted := fmt.Sprint(result)
			for _, expected := range tt.expected {
				Expect(formatted).To(ContainSubstring(expected))
			}
		})
	}

	ginkgo.It("preserves exec action metadata", func() {
		result := resolveActionResult("exec", actionResults[1].result)
		Expect(result).To(Equal(playbookExecResult{
			Stdout: "ok", Stderr: "warning", ExitCode: 2,
			Path: "/bin/sh", Args: []string{"-c", "echo ok"}, Extra: map[string]any{"commit": "abc123"},
		}))
	})

	ginkgo.It("prints only the action result for playbook run summaries", func() {
		actionID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
		var stdout bytes.Buffer

		err := PrintPlaybookActionResults(&stdout, &sdk.PlaybookSummary{
			Playbook: clientapi.Playbook{Namespace: "ops", Name: "diagnose"},
			Run:      clientapi.PlaybookRun{ID: uuid.New(), Status: clientapi.PlaybookRunStatusCompleted},
			Actions: []clientapi.PlaybookRunAction{{
				ID:     actionID,
				Name:   "HTTP Request",
				Status: clientapi.PlaybookActionStatus("completed"),
				Result: map[string]any{"code": 200, "content": "37.59.119.142"},
			}},
		})

		Expect(err).ToNot(HaveOccurred())
		Expect(stdout.String()).To(ContainSubstring("Result:"))
		Expect(stdout.String()).To(ContainSubstring("Code:"))
		Expect(stdout.String()).To(ContainSubstring("Content:"))
		Expect(stdout.String()).To(ContainSubstring("37.59.119.142"))
		Expect(stdout.String()).ToNot(ContainSubstring("playbook"))
		Expect(stdout.String()).ToNot(ContainSubstring("actions"))
		Expect(stdout.String()).ToNot(ContainSubstring(actionID.String()))
	})

	ginkgo.It("prints action results as JSON when clicky JSON output is enabled", func() {
		clicky.Flags.FormatOptions.JSON = true
		var stdout bytes.Buffer

		err := PrintPlaybookActionResults(&stdout, &sdk.PlaybookSummary{
			Actions: []clientapi.PlaybookRunAction{{
				Name:   "HTTP Request",
				Status: clientapi.PlaybookActionStatus("completed"),
				Result: map[string]any{"code": 200},
			}},
		})

		Expect(err).ToNot(HaveOccurred())
		Expect(stdout.String()).To(ContainSubstring(`"result": {`))
		Expect(stdout.String()).To(ContainSubstring(`"code": 200`))
	})

	ginkgo.It("prints playbook lists as a compact table by default", func() {
		id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
		var stdout bytes.Buffer

		err := savePlaybookList(&stdout, []clientapi.PlaybookListItem{{
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

		err := savePlaybookList(&stdout, []clientapi.PlaybookListItem{{
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
		cmd := newCachedPlaybookCommand(clientapi.PlaybookListItem{
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
		param := clientapi.PlaybookParameter{
			Name:    "container",
			Type:    "text",
			Default: `$( .config.config | jq ".spec.template.spec.containers[0].name" )`,
		}
		cmd := &cobra.Command{Use: "kubernetes-update-image"}
		value := string(param.Default)
		values := map[string]*string{"container": &value}
		cmd.Flags().StringVar(values["container"], "container", value, "")

		args, err := cachedPlaybookParamArgs(cmd, []clientapi.PlaybookParameter{param}, values)
		Expect(err).ToNot(HaveOccurred())
		Expect(args).To(BeEmpty())

		Expect(cmd.Flags().Set("container", "api")).To(Succeed())
		args, err = cachedPlaybookParamArgs(cmd, []clientapi.PlaybookParameter{param}, values)
		Expect(err).ToNot(HaveOccurred())
		Expect(args).To(Equal([]string{"container=api"}))
	})
})
