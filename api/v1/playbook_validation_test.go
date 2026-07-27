package v1

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("ParseAndValidatePlaybookSpec", func() {
	ginkgo.It("accepts schema-valid playbooks", func() {
		spec, err := ParseAndValidatePlaybookSpec([]byte(`{
			"actions": [{"name": "echo", "exec": {"script": "echo ok"}}]
		}`))

		Expect(err).ToNot(HaveOccurred())
		Expect(spec.Actions).To(HaveLen(1))
	})

	ginkgo.It("rejects properties outside the generated schema", func() {
		_, err := ParseAndValidatePlaybookSpec([]byte(`{
			"actions": [{"name": "echo", "exec": {"script": "echo ok"}}],
			"unexpected": true
		}`))

		Expect(err).To(MatchError(ContainSubstring("Additional property unexpected is not allowed")))
	})

	ginkgo.It("applies semantic validation after schema validation", func() {
		_, err := ParseAndValidatePlaybookSpec([]byte(`{
			"actions": [
				{"name": "echo", "exec": {"script": "echo one"}},
				{"name": "echo", "exec": {"script": "echo two"}}
			]
		}`))

		Expect(err).To(MatchError(ContainSubstring("all actions should have unique names")))
	})
})
