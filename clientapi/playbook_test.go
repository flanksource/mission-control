package clientapi

import (
	"encoding/json"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("Playbook DTOs", func() {
	ginkgo.It("decodes the fields needed by the remote client", func() {
		var spec PlaybookSpecSummary
		Expect(json.Unmarshal([]byte(`{
			"configs": [{"type": "Pod"}],
			"parameters": [{"name": "reason", "required": true}],
			"actions": [{"name": "query", "sql": {"query": "select 1"}}]
		}`), &spec)).To(Succeed())

		Expect(spec.Configs).To(HaveLen(1))
		Expect(spec.Parameters).To(Equal([]PlaybookParameter{{Name: "reason", Required: true}}))
		Expect(spec.Actions).To(HaveLen(1))
		Expect(spec.Actions[0]).To(HaveKey("sql"))
	})

	ginkgo.It("identifies terminal run states", func() {
		Expect(PlaybookRunStatusCompleted.Final()).To(BeTrue())
		Expect(PlaybookRunStatusFailed.Final()).To(BeTrue())
		Expect(PlaybookRunStatusRunning.Final()).To(BeFalse())
	})
})
