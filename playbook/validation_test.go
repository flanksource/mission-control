package playbook

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

var _ = Describe("PlaybookSpec validation", func() {
	It("rejects duplicate action names", func() {
		spec := v1.PlaybookSpec{
			Actions: []v1.PlaybookAction{
				{Name: "duplicate", Exec: &v1.ExecAction{Script: "echo one"}},
				{Name: "duplicate", Exec: &v1.ExecAction{Script: "echo two"}},
			},
		}

		err := spec.Validate()
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("repeated"))
	})

	It("rejects empty action name", func() {
		spec := v1.PlaybookSpec{
			Actions: []v1.PlaybookAction{
				{Name: "", Exec: &v1.ExecAction{Script: "echo hi"}},
			},
		}

		err := spec.Validate()
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("action name"))
	})

	It("rejects empty actions list", func() {
		spec := v1.PlaybookSpec{}

		err := spec.Validate()
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("at least one action"))
	})
})
