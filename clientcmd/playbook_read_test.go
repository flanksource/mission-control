package clientcmd

import (
	"bytes"
	"encoding/json"
	"time"

	"github.com/flanksource/clicky"
	"github.com/flanksource/incident-commander/clientapi"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("playbook read and delete commands", func() {
	ginkgo.It("renders execution history as the compact semantic run table", func() {
		start := time.Now().Add(-2 * time.Minute)
		end := start.Add(90 * time.Second)
		errorMessage := "step failed"
		run := clientapi.PlaybookRun{
			ID:         uuid.MustParse("12345678-1234-1234-1234-123456789012"),
			PlaybookID: uuid.MustParse("87654321-4321-4321-4321-210987654321"),
			Status:     clientapi.PlaybookRunStatusCompleted,
			Spec:       json.RawMessage(`{"raw-spec-marker":true}`),
			CreatedAt:  time.Now().Add(-time.Hour),
			StartTime:  &start,
			EndTime:    &end,
			Error:      &errorMessage,
		}
		view := playbookRun(run)

		rendered, err := clicky.Format([]playbookRun{view}, clicky.FormatOptions{Pretty: true, NoColor: true})
		Expect(err).ToNot(HaveOccurred())
		Expect(rendered).To(And(
			ContainSubstring("12345678"),
			ContainSubstring("✓"),
			MatchRegexp(`(?i)completed`),
			ContainSubstring(errorMessage),
			Not(ContainSubstring(run.PlaybookID.String())),
			Not(ContainSubstring("raw-spec-marker")),
		))

		originalJSON, err := json.Marshal(run)
		Expect(err).ToNot(HaveOccurred())
		viewJSON, err := json.Marshal(view)
		Expect(err).ToNot(HaveOccurred())
		Expect(viewJSON).To(MatchJSON(originalJSON))
	})

	ginkgo.It("renders get output as an applicable Playbook manifest", func() {
		manifest, err := playbookManifestFromItem(clientapi.PlaybookListItem{
			Namespace:   "ops",
			Name:        "restart",
			Title:       "Restart",
			Category:    "Kubernetes",
			Description: "Restarts a workload",
			Spec:        json.RawMessage(`{"actions":[{"name":"echo","exec":{"script":"echo ok"}}]}`),
		})
		Expect(err).ToNot(HaveOccurred())
		var output bytes.Buffer

		Expect(SaveOutputToWriter(&output, manifest, "", "yaml")).To(Succeed())
		Expect(output.String()).To(Equal(`apiVersion: mission-control.flanksource.com/v1
kind: Playbook
metadata:
  name: restart
  namespace: ops
spec:
  actions:
  - exec:
      script: echo ok
    name: echo
  category: Kubernetes
  description: Restarts a workload
  title: Restart

`))
	})

	ginkgo.It("accepts repeated and comma-separated history statuses", func() {
		statuses, err := parsePlaybookRunStatuses([]string{"failed,timed_out", "completed"})

		Expect(err).ToNot(HaveOccurred())
		Expect(statuses).To(Equal([]clientapi.PlaybookRunStatus{
			clientapi.PlaybookRunStatusFailed,
			clientapi.PlaybookRunStatusTimedOut,
			clientapi.PlaybookRunStatusCompleted,
		}))
	})

	ginkgo.It("rejects invalid history statuses", func() {
		_, err := parsePlaybookRunStatuses([]string{"unknown"})

		Expect(err).To(MatchError(`invalid playbook run status "unknown"`))
	})

	ginkgo.It("registers get, history, and delete under playbook", func() {
		Expect(findChildCommand(Playbook, "get")).ToNot(BeNil())
		Expect(findChildCommand(Playbook, "history")).ToNot(BeNil())
		Expect(findChildCommand(Playbook, "delete")).ToNot(BeNil())
	})
})
