package clientcmd

import (
	"bytes"
	"encoding/json"

	"github.com/flanksource/incident-commander/clientapi"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("playbook read and delete commands", func() {
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
